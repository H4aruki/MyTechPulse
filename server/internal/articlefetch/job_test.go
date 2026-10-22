package articlefetch

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/H4aruki/MyTechPulse/server/internal/article"
	"github.com/H4aruki/MyTechPulse/server/internal/provider"
	"github.com/H4aruki/MyTechPulse/server/internal/provider/qiita"
)

var jobNow = time.Date(2026, 10, 22, 3, 0, 0, 0, time.UTC)

func page(rate int, arts ...article.Article) qiita.Page {
	return qiita.Page{Articles: arts, RateRemaining: rate}
}

func qErr(kind error, status int) error {
	return &provider.Error{Provider: "qiita", Kind: kind, StatusCode: status}
}

func zErr(kind error, status int) error {
	return &provider.Error{Provider: "zenn", Kind: kind, StatusCode: status}
}

type jobFixture struct {
	store *fakeJobStore
	q     *fakeQiita
	z     *fakeZenn
	clock *fakeClock
	logs  *bytes.Buffer
	job   *Job
}

func newJobFixture() *jobFixture {
	f := &jobFixture{
		store: &fakeJobStore{tags: []TagRef{{ID: 1, Name: "Go"}, {ID: 2, Name: "Spring Boot"}}},
		q:     &fakeQiita{perPage: 2},
		z:     &fakeZenn{fn: func(string) ([]article.Article, error) { return nil, nil }},
		clock: newAutoClock(jobNow),
		logs:  &bytes.Buffer{},
	}
	f.q.fn = func(time.Time, int) (qiita.Page, error) { return page(-1), nil }
	f.job = &Job{
		Store: f.store, Qiita: f.q, Zenn: f.z, Clock: f.clock,
		Logger:           slog.New(slog.NewTextHandler(&lockedWriter{w: f.logs}, nil)),
		QiitaMaxRequests: 300,
	}
	return f
}

type lockedWriter struct {
	mu sync.Mutex
	w  *bytes.Buffer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func TestQiitaDaysCoverRetentionAndJapanToday(t *testing.T) {
	days := qiitaDays(jobNow) // 2026-10-22 03:00 UTC = 12:00 JST
	if len(days) != 6 {
		t.Fatalf("days = %v", days)
	}
	if first, last := days[0].Format("2006-01-02"), days[len(days)-1].Format("2006-01-02"); first != "2026-10-22" || last != "2026-10-17" {
		t.Fatalf("first = %s, last = %s", first, last)
	}
	// 日本時間では翌日になっている時刻（UTCの21:00 = 日本時間の翌6:00）は、日本時間の今日まで含める
	late := time.Date(2026, 10, 21, 21, 0, 0, 0, time.UTC)
	days = qiitaDays(late)
	if days[0].Format("2006-01-02") != "2026-10-22" || days[len(days)-1].Format("2006-01-02") != "2026-10-16" {
		t.Fatalf("days = %v", days)
	}
}

func TestQiitaPagesPerDayStopAtShortPage(t *testing.T) {
	f := newJobFixture()
	f.q.fn = func(day time.Time, p int) (qiita.Page, error) {
		id := day.Format("0102") + "-" + strconv.Itoa(p)
		if day.Day() == 22 && p == 1 { // 満杯のページの次は取りに行く
			return page(900, qArticle(id+"a"), qArticle(id+"b")), nil
		}
		return page(900, qArticle(id+"a")), nil
	}
	res := f.job.Execute(context.Background())
	if res.Status != StatusSucceeded || res.Message != "" {
		t.Fatalf("res = %+v", res)
	}
	// 6日分 + 10-22 の2ページ目
	if res.QiitaRequests != 7 || f.q.callCount() != 7 {
		t.Fatalf("requests = %d, calls = %v", res.QiitaRequests, f.q.calls)
	}
	if res.Upserted != 8 || len(f.store.saved) != 8 {
		t.Fatalf("upserted = %d, saved = %d", res.Upserted, len(f.store.saved))
	}
	var seen []string
	for _, c := range f.q.calls {
		if strings.HasPrefix(c, "2026-10-22#") {
			seen = append(seen, c)
		}
	}
	sort.Strings(seen)
	if !reflect.DeepEqual(seen, []string{"2026-10-22#1", "2026-10-22#2"}) {
		t.Fatalf("10-22 calls = %v", seen)
	}
}

func TestQiitaCallCapStopsAndMarksPartial(t *testing.T) {
	f := newJobFixture()
	f.job.QiitaMaxRequests = 3
	var n atomic.Int64
	f.q.fn = func(time.Time, int) (qiita.Page, error) { // いつも満杯
		k := n.Add(1)
		return page(900, qArticle("a"+strconv.FormatInt(k, 10)), qArticle("b"+strconv.FormatInt(k, 10))), nil
	}
	res := f.job.Execute(context.Background())
	if res.QiitaRequests != 3 || f.q.callCount() != 3 {
		t.Fatalf("requests = %d, calls = %d", res.QiitaRequests, f.q.callCount())
	}
	if res.Status != StatusPartial || !strings.Contains(res.Message, "Qiitaの呼び出しの上限") {
		t.Fatalf("res = %+v", res)
	}
	if res.Upserted != 6 {
		t.Fatalf("upserted = %d", res.Upserted)
	}
}

func TestQiitaCallCapCountsRetries(t *testing.T) {
	f := newJobFixture()
	f.job.QiitaMaxRequests = 5
	f.q.fn = func(time.Time, int) (qiita.Page, error) { return page(-1), qErr(provider.ErrUnavailable, 503) }
	res := f.job.Execute(context.Background())
	if res.QiitaRequests != 5 || f.q.callCount() != 5 {
		t.Fatalf("requests = %d", res.QiitaRequests)
	}
	if res.Status != StatusFailed {
		t.Fatalf("res = %+v", res)
	}
}

func TestQiitaStopsWhenRateRemainingIsLow(t *testing.T) {
	f := newJobFixture()
	f.q.fn = func(day time.Time, p int) (qiita.Page, error) {
		return page(qiitaMinRemaining-1, qArticle("a"+day.Format("0102")+strconv.Itoa(p)), qArticle("b"+day.Format("0102")+strconv.Itoa(p))), nil
	}
	res := f.job.Execute(context.Background())
	// 並列4本が最初の呼び出しを終えた時点で止まる。以後は通信しない。
	if n := f.q.callCount(); n < 1 || n > qiitaWorkers {
		t.Fatalf("calls = %d", n)
	}
	if res.Status != StatusPartial || !strings.Contains(res.Message, "残りの呼び出し回数が少ない") {
		t.Fatalf("res = %+v", res)
	}
	if res.Upserted == 0 {
		t.Fatal("取れた分は保存する")
	}
}

func TestQiitaRateRemainingAtLimitDoesNotStop(t *testing.T) {
	f := newJobFixture()
	f.q.fn = func(day time.Time, p int) (qiita.Page, error) {
		return page(qiitaMinRemaining, qArticle("a"+day.Format("0102"))), nil
	}
	if res := f.job.Execute(context.Background()); res.Status != StatusSucceeded || f.q.callCount() != 6 {
		t.Fatalf("res = %+v, calls = %d", res, f.q.callCount())
	}
}

func TestQiitaRateLimitedResponseStopsRemainingWork(t *testing.T) {
	f := newJobFixture()
	f.q.fn = func(time.Time, int) (qiita.Page, error) { return page(0), qErr(provider.ErrRateLimited, 429) }
	res := f.job.Execute(context.Background())
	if n := f.q.callCount(); n < 1 || n > qiitaWorkers {
		t.Fatalf("429の後も通信している: calls = %d", n)
	}
	if res.Status != StatusFailed || !strings.Contains(res.Message, "qiita: provider rate limited (status 429)") {
		t.Fatalf("res = %+v", res)
	}
	if len(f.clock.Waits()) != 0 {
		t.Fatalf("429は再試行しない: waits = %v", f.clock.Waits())
	}
}

func TestQiitaDayFailureAfterRetriesIsPartial(t *testing.T) {
	f := newJobFixture()
	f.q.fn = func(day time.Time, p int) (qiita.Page, error) {
		if day.Day() == 20 {
			return page(-1), qErr(provider.ErrUnavailable, 502)
		}
		return page(-1, qArticle("a"+day.Format("0102"))), nil
	}
	res := f.job.Execute(context.Background())
	// 5日分は1回ずつ、1日は 最初の1回 + 再試行3回
	if res.Status != StatusPartial || res.QiitaRequests != 5+4 {
		t.Fatalf("res = %+v", res)
	}
	if res.Message != "qiita: provider unavailable (status 502)" {
		t.Fatalf("message = %q", res.Message)
	}
	if res.Upserted != 5 {
		t.Fatalf("upserted = %d", res.Upserted)
	}
}

func TestQiitaClientErrorsAreNotRetried(t *testing.T) {
	f := newJobFixture()
	f.q.fn = func(time.Time, int) (qiita.Page, error) { return page(-1), qErr(provider.ErrUnavailable, 404) }
	res := f.job.Execute(context.Background())
	if res.QiitaRequests != 6 || len(f.clock.Waits()) != 0 {
		t.Fatalf("requests = %d, waits = %v", res.QiitaRequests, f.clock.Waits())
	}
}

func TestQiitaPageLimitWarning(t *testing.T) {
	f := newJobFixture()
	f.store.fetchTags = nil
	var calls atomic.Int64
	f.q.fn = func(day time.Time, p int) (qiita.Page, error) {
		if day.Day() != 22 {
			return page(-1), nil
		}
		k := calls.Add(1)
		return page(-1, qArticle("x"+strconv.FormatInt(k, 10)), qArticle("y"+strconv.FormatInt(k, 10))), nil
	}
	res := f.job.Execute(context.Background())
	if !strings.Contains(res.Message, "1日分が検索の上限に達した") {
		t.Fatalf("res = %+v", res)
	}
	if calls.Load() != qiitaMaxPage {
		t.Fatalf("pages = %d, want %d", calls.Load(), qiitaMaxPage)
	}
}

func TestQiitaTagsAreMatchedAndUnknownTagsAreIgnored(t *testing.T) {
	f := newJobFixture()
	f.q.fn = func(day time.Time, p int) (qiita.Page, error) {
		if day.Day() != 22 {
			return page(-1), nil
		}
		return page(-1,
			qArticle("k1", "go", "Unknown", "SpringBoot", "GO"),
			qArticle("k2", "totally-unknown"),
		), nil
	}
	res := f.job.Execute(context.Background())
	if res.Status != StatusSucceeded {
		t.Fatalf("res = %+v", res)
	}
	byID := map[string][]int32{}
	for _, r := range f.store.saved {
		byID[r.Article.ExternalID] = r.TagIDs
	}
	if !reflect.DeepEqual(byID["k1"], []int32{1, 2}) {
		t.Fatalf("k1 tags = %v", byID["k1"])
	}
	// 知らないタグしかない記事も、記事は保存する(タグは0件)
	if tags, ok := byID["k2"]; !ok || len(tags) != 0 {
		t.Fatalf("k2 = %v, %v", tags, ok)
	}
}

func TestZennUsesOnlyTheFetchTagAndSpacesCalls(t *testing.T) {
	f := newJobFixture()
	f.q.fn = func(time.Time, int) (qiita.Page, error) { return page(-1), nil }
	f.store.fetchTags = []FetchTag{
		{ID: 1, Name: "Go", ZennTopic: "go"},
		{ID: 2, Name: "Spring Boot", ZennTopic: "springboot"},
		{ID: 3, Name: "Rust", ZennTopic: "rust"},
	}
	f.z.fn = func(topic string) ([]article.Article, error) {
		return []article.Article{zArticle(topic + "-1"), zArticle(topic + "-2")}, nil
	}
	res := f.job.Execute(context.Background())
	if res.Status != StatusSucceeded || res.ZennRequests != 3 || res.Upserted != 6 {
		t.Fatalf("res = %+v", res)
	}
	want := map[string]int32{"go": 1, "springboot": 2, "rust": 3}
	for _, r := range f.store.saved {
		topic := strings.SplitN(r.Article.ExternalID, "-", 2)[0]
		if !reflect.DeepEqual(r.TagIDs, []int32{want[topic]}) {
			t.Fatalf("%s tags = %v, want [%d]", r.Article.ExternalID, r.TagIDs, want[topic])
		}
	}
	// 開始は0.5秒以上あける: 時計を止めた状態では、2回目以降の待ちが 0.5s, 1s になる
	waits := f.clock.Waits()
	sort.Slice(waits, func(i, j int) bool { return waits[i] < waits[j] })
	if !reflect.DeepEqual(waits, []time.Duration{500 * time.Millisecond, time.Second}) {
		t.Fatalf("waits = %v", waits)
	}
}

func TestZennRunsAtMostTwoAtOnce(t *testing.T) {
	f := newJobFixture()
	for i := 0; i < 20; i++ {
		f.store.fetchTags = append(f.store.fetchTags, FetchTag{ID: int32(i + 1), Name: "t", ZennTopic: "t" + strconv.Itoa(i)})
	}
	var cur, peak atomic.Int64
	f.z.fn = func(string) ([]article.Article, error) {
		n := cur.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(3 * time.Millisecond)
		cur.Add(-1)
		return nil, nil
	}
	f.job.Execute(context.Background())
	if peak.Load() != int64(zennWorkers) {
		t.Fatalf("同時に動いた最大 = %d, want %d", peak.Load(), zennWorkers)
	}
}

func TestZennStopsAfterConsecutiveFailures(t *testing.T) {
	f := newJobFixture()
	for i := 0; i < 40; i++ {
		f.store.fetchTags = append(f.store.fetchTags, FetchTag{ID: int32(i + 1), Name: "t", ZennTopic: "t" + strconv.Itoa(i)})
	}
	f.z.fn = func(string) ([]article.Article, error) { return nil, zErr(provider.ErrUnavailable, 500) }
	res := f.job.Execute(context.Background())
	if n := f.z.callCount(); n > (zennMaxConsecutiveFailures+zennWorkers)*(maxRetries+1) {
		t.Fatalf("失敗が続いても通信し続けた: calls = %d", n)
	}
	if !strings.Contains(res.Message, "Zennの失敗が続いた") || !strings.Contains(res.Message, "zenn: provider unavailable (status 500)") {
		t.Fatalf("message = %q", res.Message)
	}
	if res.Status != StatusPartial { // Qiitaは(空でも)成功している
		t.Fatalf("status = %s", res.Status)
	}
}

func TestZennRecordsTopicsWithNoArticles(t *testing.T) {
	f := newJobFixture()
	f.store.fetchTags = []FetchTag{{ID: 1, Name: "C++", ZennTopic: "c++"}, {ID: 2, Name: "Go", ZennTopic: "go"}}
	f.z.fn = func(topic string) ([]article.Article, error) {
		if topic == "go" {
			return []article.Article{zArticle("g")}, nil
		}
		return nil, nil
	}
	res := f.job.Execute(context.Background())
	if res.Status != StatusSucceeded {
		t.Fatalf("res = %+v", res)
	}
	if !strings.Contains(f.logs.String(), "zenn topics with no articles") || !strings.Contains(f.logs.String(), "c++") {
		t.Fatalf("logs = %s", f.logs.String())
	}
}

func TestJobStatusFromBothProviders(t *testing.T) {
	okQiita := func(day time.Time, p int) (qiita.Page, error) { return page(-1, qArticle("a"+day.Format("0102"))), nil }
	failQiita := func(time.Time, int) (qiita.Page, error) { return page(-1), qErr(provider.ErrUnavailable, 500) }
	okZenn := func(topic string) ([]article.Article, error) { return []article.Article{zArticle(topic)}, nil }
	failZenn := func(string) ([]article.Article, error) { return nil, zErr(provider.ErrUnavailable, 500) }
	for _, tc := range []struct {
		name   string
		q      func(time.Time, int) (qiita.Page, error)
		z      func(string) ([]article.Article, error)
		topics int
		want   string
	}{
		{"両方成功", okQiita, okZenn, 2, StatusSucceeded},
		{"Qiitaだけ全滅", failQiita, okZenn, 2, StatusPartial},
		{"Zennだけ全滅", okQiita, failZenn, 2, StatusPartial},
		{"両方全滅", failQiita, failZenn, 2, StatusFailed},
		{"Zennの対象が0件でQiitaが全滅", failQiita, okZenn, 0, StatusFailed},
		{"Zennの対象が0件でQiitaが成功", okQiita, okZenn, 0, StatusSucceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newJobFixture()
			f.q.fn, f.z.fn = tc.q, tc.z
			for i := 0; i < tc.topics; i++ {
				f.store.fetchTags = append(f.store.fetchTags, FetchTag{ID: int32(i + 1), Name: "t", ZennTopic: "t" + strconv.Itoa(i)})
			}
			if res := f.job.Execute(context.Background()); res.Status != tc.want {
				t.Fatalf("res = %+v, want %s", res, tc.want)
			}
		})
	}
}

func TestJobSkipsArticlesOlderThanRetentionAndDeletesExpired(t *testing.T) {
	f := newJobFixture()
	f.store.deleted = 7
	f.store.fetchTags = []FetchTag{{ID: 1, Name: "Go", ZennTopic: "go"}}
	oldQ := qArticle("old")
	oldQ.PublishedAt = jobNow.Add(-qiitaRetention - time.Minute)
	f.q.fn = func(day time.Time, p int) (qiita.Page, error) {
		if day.Day() == 22 && p == 1 {
			return page(-1, qArticle("new"), oldQ), nil
		}
		return page(-1), nil
	}
	oldZ, newZ := zArticle("zold"), zArticle("znew")
	oldZ.PublishedAt = jobNow.Add(-zennRetention - time.Minute)
	newZ.PublishedAt = jobNow.Add(-zennRetention + time.Hour)
	f.z.fn = func(string) ([]article.Article, error) { return []article.Article{oldZ, newZ}, nil }
	res := f.job.Execute(context.Background())
	var ids []string
	for _, r := range f.store.saved {
		ids = append(ids, r.Article.ExternalID)
	}
	sort.Strings(ids)
	if !reflect.DeepEqual(ids, []string{"new", "znew"}) {
		t.Fatalf("saved = %v", ids)
	}
	if got := f.store.deletes["qiita"]; !got.Equal(jobNow.Add(-5 * 24 * time.Hour)) {
		t.Fatalf("qiita cutoff = %v", got)
	}
	if got := f.store.deletes["zenn"]; !got.Equal(jobNow.Add(-30 * 24 * time.Hour)) {
		t.Fatalf("zenn cutoff = %v", got)
	}
	if res.Deleted != 14 {
		t.Fatalf("deleted = %d", res.Deleted)
	}
}

func TestJobRetentionRunsEvenWhenProvidersFail(t *testing.T) {
	f := newJobFixture()
	f.q.fn = func(time.Time, int) (qiita.Page, error) { return page(-1), qErr(provider.ErrUnavailable, 500) }
	res := f.job.Execute(context.Background())
	if res.Status != StatusFailed || len(f.store.deletes) != 2 {
		t.Fatalf("res = %+v, deletes = %v", res, f.store.deletes)
	}
}

func TestJobSaveFailureStopsRunAndHidesDBError(t *testing.T) {
	f := newJobFixture()
	f.store.saveErr = errors.New(`dial tcp: password=hunter2 host=db.internal connection refused`)
	f.q.fn = func(day time.Time, p int) (qiita.Page, error) {
		return page(-1, qArticle("a"+day.Format("0102")+strconv.Itoa(p)), qArticle("b"+day.Format("0102")+strconv.Itoa(p))), nil
	}
	res := f.job.Execute(context.Background())
	if res.Status != StatusFailed || !strings.Contains(res.Message, "記事の保存に失敗しました") {
		t.Fatalf("res = %+v", res)
	}
	for _, leak := range []string{"hunter2", "db.internal", "dial tcp"} {
		if strings.Contains(res.Message, leak) || strings.Contains(f.logs.String(), leak) {
			t.Fatalf("%q が漏れている: message = %q, logs = %s", leak, res.Message, f.logs.String())
		}
	}
	if len(f.store.deletes) != 2 || f.store.deleteCtxErr != nil {
		t.Fatalf("保存に失敗しても、取り消されていない ctx で期限切れを消す: deletes = %v, ctxErr = %v", f.store.deletes, f.store.deleteCtxErr)
	}
	if n := f.q.callCount(); n > qiitaWorkers*2 {
		t.Fatalf("保存に失敗しても通信し続けた: calls = %d", n)
	}
}

func TestJobTagListFailure(t *testing.T) {
	f := newJobFixture()
	f.store.listErr = errors.New("secret detail")
	res := f.job.Execute(context.Background())
	if res.Status != StatusFailed || strings.Contains(res.Message, "secret") || f.q.callCount() != 0 {
		t.Fatalf("res = %+v", res)
	}
}

func TestJobCanceledContext(t *testing.T) {
	f := newJobFixture()
	f.store.fetchTags = []FetchTag{{ID: 1, Name: "Go", ZennTopic: "go"}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res := f.job.Execute(ctx)
	if res.Status != StatusFailed || !strings.Contains(res.Message, "中断") {
		t.Fatalf("res = %+v", res)
	}
	if f.q.callCount() != 0 || f.z.callCount() != 0 {
		t.Fatal("取り消し後は通信しない")
	}
	if len(f.store.deletes) != 2 || f.store.deleteCtxErr != nil || !f.store.deleteHasDead {
		t.Fatalf("中断されても、期限付きの別の ctx で期限切れを消す: deletes = %v, ctxErr = %v, deadline = %v",
			f.store.deletes, f.store.deleteCtxErr, f.store.deleteHasDead)
	}
}

func TestJobTimeoutMessage(t *testing.T) {
	f := newJobFixture()
	f.store.deleted = 3
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	res := f.job.Execute(ctx)
	if !strings.Contains(res.Message, "実行時間の上限") {
		t.Fatalf("res = %+v", res)
	}
	if res.Deleted != 6 || f.store.deleteCtxErr != nil {
		t.Fatalf("時間切れでも期限切れを消す: deleted = %d, ctxErr = %v", res.Deleted, f.store.deleteCtxErr)
	}
}

func TestJobRetentionFailureIsOnlyLogged(t *testing.T) {
	f := newJobFixture()
	f.store.deleteErr = errors.New("secret delete detail")
	res := f.job.Execute(context.Background())
	if res.Status != StatusSucceeded || res.Deleted != 0 || strings.Contains(res.Message, "secret") {
		t.Fatalf("res = %+v", res)
	}
	if !strings.Contains(f.logs.String(), "retention delete failed") || strings.Contains(f.logs.String(), "secret") {
		t.Fatalf("logs = %s", f.logs.String())
	}
}

func TestQiitaOtherInvalidResponseStillGivesUpTheDay(t *testing.T) {
	f := newJobFixture()
	f.q.fn = func(day time.Time, p int) (qiita.Page, error) {
		if day.Day() == 22 && p == 1 {
			return page(-1), qErr(provider.ErrInvalidResponse, 200)
		}
		return page(-1), nil
	}
	res := f.job.Execute(context.Background())
	if res.QiitaRequests != 6 || strings.Contains(res.Message, "大きすぎる") {
		t.Fatalf("res = %+v, calls = %v", res, f.q.calls)
	}
}

func TestQiitaSkippedArticlesAreCountedAndLogged(t *testing.T) {
	f := newJobFixture()
	f.q.fn = func(day time.Time, p int) (qiita.Page, error) {
		if day.Day() == 22 && p == 1 {
			pg := page(-1, qArticle("ok"))
			pg.Skipped = 1 // 飛ばした記事も含めて満杯(2件)なので、次のページも取りに行く
			return pg, nil
		}
		return page(-1), nil
	}
	res := f.job.Execute(context.Background())
	if res.Status != StatusSucceeded || res.Upserted != 1 || res.QiitaRequests != 7 {
		t.Fatalf("res = %+v, calls = %v", res, f.q.calls)
	}
	logs := f.logs.String()
	if !strings.Contains(logs, "qiita_invalid_articles=1") || !strings.Contains(logs, "missing fields skipped") {
		t.Fatalf("logs = %s", logs)
	}
}

func TestDescribeNeverEchoesUnknownErrors(t *testing.T) {
	for _, err := range []error{
		errors.New("GET https://qiita.com/api/v2/items?token=abc failed"),
		errors.New("Authorization: Bearer abc"),
	} {
		if got := describe(err); got != msgFetchFailed {
			t.Errorf("describe(%v) = %q", err, got)
		}
	}
	// 万一 provider.Error の文にURLが混ざっても、そのまま記録しない
	if got := describe(&provider.Error{Provider: "https://evil.example/?token=1", Kind: provider.ErrTimeout}); got != msgFetchFailed {
		t.Errorf("describe = %q", got)
	}
}

func TestJoinMessagesTruncates(t *testing.T) {
	long := strings.Repeat("あ", 600)
	if got := []rune(joinMessages([]string{long})); len(got) != maxMessageRunes {
		t.Fatalf("len = %d", len(got))
	}
}
