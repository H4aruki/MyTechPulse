package articlefetch

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/H4aruki/MyTechPulse/server/internal/article"
	"github.com/H4aruki/MyTechPulse/server/internal/provider"
)

// 取得の決まり。Issue #66 の決定事項なので、環境変数では変えられない。
const (
	qiitaRetention = 5 * 24 * time.Hour
	zennRetention  = 30 * 24 * time.Hour
	qiitaWorkers   = 4
	zennWorkers    = 2
	zennGap        = 500 * time.Millisecond
	// qiitaMaxPage はQiitaの検索で指定できるページ番号の上限。1日分がここに届いたら取りこぼしの可能性がある。
	qiitaMaxPage = 100
	// qiitaMinRemaining を下回ったら、フィードの呼び出しに残すため、Qiitaの取得を止める。
	qiitaMinRemaining = 100
	// zennMaxConsecutiveFailures 回続けて失敗したら、Zennが落ちているとみて残りを取りに行かない。
	zennMaxConsecutiveFailures = 10
	// maxMessageRunes は error_message の最大文字数。
	maxMessageRunes = 500
	// maxLoggedTopics は、記事が0件だったトピックとして、ログに出す最大数。
	maxLoggedTopics = 30
	// retainTimeout は期限切れの削除にかける時間の上限。実行の時間切れ・中断の後でも削除できるよう、別の期限にする。
	retainTimeout = 30 * time.Second
)

// 固定の説明文。fetch_runs.error_message に入れてよいのは、これと provider.Error の文だけ。
const (
	msgSaveFailed  = "記事の保存に失敗しました"
	msgCallLimit   = "Qiitaの呼び出しの上限に達したため、途中で止めました"
	msgLowRate     = "Qiitaの残りの呼び出し回数が少ないため、途中で止めました"
	msgTimeout     = "実行時間の上限に達しました"
	msgCanceled    = "実行が中断されました"
	msgFetchFailed = "取得に失敗しました"
	msgPageLimit   = "Qiitaの1日分が検索の上限に達したため、取りこぼしの可能性があります"
	msgZennDown    = "Zennの失敗が続いたため、残りのトピックを取りに行きませんでした"
	msgTagsFailed  = "タグの一覧を読めませんでした"
	msgPageTooBig  = "Qiitaの応答が大きすぎるページを飛ばしました"
)

// Job は1回の取得を行う。
type Job struct {
	Store  JobStore
	Qiita  QiitaSource
	Zenn   ZennSource
	Clock  Clock
	Logger *slog.Logger
	// QiitaMaxRequests は1回の実行でQiitaへ通信してよい回数（再試行を含む）。
	QiitaMaxRequests int
	// Jitter は再試行の待ちのゆらぎ（-1以上1未満）。nilならゆらぎなし。
	Jitter func() float64
}

var _ Executor = (*Job)(nil)

// outcome は1つの提供元の結果。
type outcome struct {
	mu        sync.Mutex
	ok        int // 取れた単位（Qiitaは1ページ、Zennは1トピック）
	failed    int // 再試行しても取れなかった単位
	truncated bool
	upserted  int
	invalid   int // 必須項目が欠けるなどで、提供元の応答から飛ばした記事
	messages  []string
	zero      []string // Zennで記事が0件だったトピック
}

func (o *outcome) addOK(saved int) {
	o.mu.Lock()
	o.ok++
	o.upserted += saved
	o.mu.Unlock()
}

func (o *outcome) addFailed(err error) {
	o.mu.Lock()
	o.failed++
	o.addMessageLocked(describe(err))
	o.mu.Unlock()
}

// addFailedMsg は失敗単位を1つ数え、固定の説明文を残す。
func (o *outcome) addFailedMsg(msg string) {
	o.mu.Lock()
	o.failed++
	o.addMessageLocked(msg)
	o.mu.Unlock()
}

func (o *outcome) addInvalid(n int) {
	o.mu.Lock()
	o.invalid += n
	o.mu.Unlock()
}

func (o *outcome) truncate(msg string) {
	o.mu.Lock()
	o.truncated = true
	if msg != "" {
		o.addMessageLocked(msg)
	}
	o.mu.Unlock()
}

func (o *outcome) warn(msg string) {
	o.mu.Lock()
	o.addMessageLocked(msg)
	o.mu.Unlock()
}

func (o *outcome) addMessageLocked(msg string) {
	for _, m := range o.messages {
		if m == msg {
			return
		}
	}
	o.messages = append(o.messages, msg)
}

// nothing は、1つも取れず、失敗または途中打ち切りがあったか。
func (o *outcome) nothing() bool { return o.ok == 0 && (o.failed > 0 || o.truncated) }

// dbFailure は保存の失敗を1回だけ記録し、実行全体を止める。
type dbFailure struct {
	failed atomic.Bool
	cancel context.CancelFunc
}

func (d *dbFailure) fail() {
	d.failed.Store(true)
	d.cancel()
}

// Execute は1回の取得を行い、結果を返す。結果の記録（fetch_runs）は呼び出し側が行う。
func (j *Job) Execute(ctx context.Context) Result {
	log := j.logger()
	tags, err := j.Store.ListTags(ctx)
	var fetchTags []FetchTag
	if err == nil {
		fetchTags, err = j.Store.ListFetchTags(ctx)
	}
	if err != nil {
		log.Error("article fetch: tag list failed")
		return Result{Status: StatusFailed, Message: msgTagsFailed}
	}
	matcher := NewTagMatcher(tags)

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	db := &dbFailure{cancel: cancel}
	now := j.Clock.Now()

	var q, z outcome
	var qCalls, zCalls counter
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		j.fetchQiita(runCtx, now, matcher, db, &q, &qCalls)
	}()
	go func() {
		defer wg.Done()
		j.fetchZenn(runCtx, now, fetchTags, db, &z, &zCalls)
	}()
	wg.Wait()

	res := Result{QiitaRequests: qCalls.count(), ZennRequests: zCalls.count(), Upserted: q.upserted + z.upserted}
	// 保存の失敗・時間切れ・中断の実行でも、期限切れの記事は消す。
	res.Deleted = j.retain(ctx, now)

	var msgs []string
	msgs = append(msgs, q.messages...)
	msgs = append(msgs, z.messages...)
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		msgs = append(msgs, msgTimeout)
	case ctx.Err() != nil:
		msgs = append(msgs, msgCanceled)
	}
	if db.failed.Load() {
		msgs = append(msgs, msgSaveFailed)
	}
	res.Message = joinMessages(msgs)

	anyProblem := q.failed > 0 || z.failed > 0 || q.truncated || z.truncated || ctx.Err() != nil
	switch {
	case db.failed.Load():
		res.Status = StatusFailed
	case q.ok+z.ok == 0 && anyProblem:
		res.Status = StatusFailed
	case anyProblem:
		res.Status = StatusPartial
	default:
		res.Status = StatusSucceeded
	}

	log.Info("article fetch finished",
		"status", res.Status,
		"qiita_requests", res.QiitaRequests, "zenn_requests", res.ZennRequests,
		"upserted", res.Upserted, "deleted", res.Deleted,
		"qiita_pages_ok", q.ok, "qiita_failed", q.failed, "qiita_invalid_articles", q.invalid,
		"zenn_topics_ok", z.ok, "zenn_failed", z.failed, "zenn_topics_empty", len(z.zero))
	if q.invalid > 0 {
		log.Warn("article fetch: qiita articles with missing fields skipped", "count", q.invalid)
	}
	if len(z.zero) > 0 {
		shown := z.zero
		if len(shown) > maxLoggedTopics {
			shown = shown[:maxLoggedTopics]
		}
		log.Info("article fetch: zenn topics with no articles", "count", len(z.zero), "topics", shown)
	}
	return res
}

func (j *Job) logger() *slog.Logger {
	if j.Logger != nil {
		return j.Logger
	}
	return slog.New(slog.DiscardHandler)
}

func (j *Job) newCaller(p *pacer, calls *counter) *caller {
	return &caller{clock: j.Clock, jitter: j.Jitter, pacer: p, calls: calls}
}

// retain は保持期間を過ぎた記事を消す。消した数を返す。失敗してもログに種類だけ残して続ける（削除は次の回でもできる）。
// 実行の ctx が時間切れ・中断でも消せるよう、取り消しを切り離し、retainTimeout の期限を付けて使う。
func (j *Job) retain(ctx context.Context, now time.Time) int {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), retainTimeout)
	defer cancel()
	total := 0
	for _, r := range []struct {
		source string
		keep   time.Duration
	}{{"qiita", qiitaRetention}, {"zenn", zennRetention}} {
		n, err := j.Store.DeleteExpired(ctx, r.source, now.Add(-r.keep))
		if err != nil {
			j.logger().Error("article fetch: retention delete failed", "source", r.source)
			continue
		}
		total += int(n)
	}
	return total
}

// save は1回分の記事を保存する。保存に失敗したら実行全体を止める。
func (j *Job) save(ctx context.Context, recs []Record, db *dbFailure) (int, bool) {
	if len(recs) == 0 {
		return 0, true
	}
	res, err := j.Store.SaveArticles(ctx, recs)
	if err != nil {
		if ctx.Err() == nil {
			j.logger().Error("article fetch: save failed")
			db.fail()
		}
		return 0, false
	}
	if res.Skipped > 0 {
		j.logger().Warn("article fetch: articles skipped", "count", res.Skipped)
	}
	return res.Saved, true
}

// qiitaDays は、Qiitaを問い合わせる日付(UTCの0時で表す)を新しい順に返す。
// 保持期間の始まりの日から、日本時間の「今日」まで。Qiitaが日付をUTCと日本時間のどちらで解釈しても、
// 保持期間内の記事が漏れない。
func qiitaDays(now time.Time) []time.Time {
	day := func(t time.Time) time.Time {
		y, m, d := t.UTC().Date()
		return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	}
	start, end := day(now.Add(-qiitaRetention)), day(now.Add(9*time.Hour))
	var days []time.Time
	for d := end; !d.Before(start); d = d.AddDate(0, 0, -1) {
		days = append(days, d)
	}
	return days
}

func (j *Job) fetchQiita(ctx context.Context, now time.Time, m *TagMatcher, db *dbFailure, out *outcome, calls *counter) {
	calls.max = int64(j.QiitaMaxRequests)
	c := j.newCaller(nil, calls)
	cutoff := now.Add(-qiitaRetention)
	var stopped atomic.Bool // 上限・429・残り回数の不足で、以後の通信をやめる

	days := make(chan time.Time)
	var wg sync.WaitGroup
	for i := 0; i < qiitaWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for day := range days {
				if stopped.Load() || ctx.Err() != nil {
					out.truncate("")
					continue
				}
				j.qiitaDay(ctx, c, day, cutoff, m, db, out, &stopped)
			}
		}()
	}
	for _, d := range qiitaDays(now) {
		days <- d
	}
	close(days)
	wg.Wait()
}

func (j *Job) qiitaDay(ctx context.Context, c *caller, day, cutoff time.Time, m *TagMatcher, db *dbFailure, out *outcome, stopped *atomic.Bool) {
	for page := 1; page <= qiitaMaxPage; page++ {
		if stopped.Load() || ctx.Err() != nil {
			out.truncate("")
			return
		}
		var pg = struct {
			articles []article.Article
			full     bool
			invalid  int
		}{}
		err := c.do(ctx, func(ctx context.Context) error {
			p, err := j.Qiita.ListCreated(ctx, day, page)
			if p.RateRemaining >= 0 && p.RateRemaining < qiitaMinRemaining {
				if stopped.CompareAndSwap(false, true) {
					out.truncate(msgLowRate)
				}
			}
			if err != nil {
				return err
			}
			pg.articles, pg.full, pg.invalid = p.Articles, j.Qiita.FullPage(p), p.Skipped
			return nil
		})
		switch {
		case err == nil:
		case errors.Is(err, provider.ErrResponseTooLarge):
			// 再試行しても同じ大きさなので、このページだけ飛ばして次のページへ進む。
			out.addFailedMsg(msgPageTooBig)
			if page == qiitaMaxPage {
				out.warn(msgPageLimit)
			}
			continue
		case errors.Is(err, errCallLimit):
			stopped.Store(true)
			out.truncate(msgCallLimit)
			return
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			out.truncate("")
			return
		case errors.Is(err, provider.ErrRateLimited):
			// Retry-After まで待つと次の定時をまたぐことがあるため、残りは打ち切る。
			stopped.Store(true)
			out.addFailed(err)
			return
		default:
			out.addFailed(err)
			return // この日はあきらめ、次の日へ
		}

		if pg.invalid > 0 {
			out.addInvalid(pg.invalid)
		}
		recs := make([]Record, 0, len(pg.articles))
		for _, a := range pg.articles {
			if a.PublishedAt.Before(cutoff) {
				continue
			}
			recs = append(recs, Record{Article: a, TagIDs: m.MatchAll(a.Tags)})
		}
		saved, ok := j.save(ctx, recs, db)
		if !ok {
			return
		}
		out.addOK(saved)
		if !pg.full {
			return
		}
		if page == qiitaMaxPage {
			out.warn(msgPageLimit)
		}
	}
}

func (j *Job) fetchZenn(ctx context.Context, now time.Time, tags []FetchTag, db *dbFailure, out *outcome, calls *counter) {
	if len(tags) == 0 {
		return
	}
	c := j.newCaller(newPacer(j.Clock, zennGap), calls)
	cutoff := now.Add(-zennRetention)
	var consecutive atomic.Int64
	var down atomic.Bool

	jobs := make(chan FetchTag)
	var wg sync.WaitGroup
	for i := 0; i < zennWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for tag := range jobs {
				if down.Load() || ctx.Err() != nil {
					out.truncate("")
					continue
				}
				var arts []article.Article
				err := c.do(ctx, func(ctx context.Context) error {
					var err error
					arts, err = j.Zenn.SearchTopic(ctx, tag.ZennTopic)
					return err
				})
				switch {
				case err == nil:
				case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
					out.truncate("")
					continue
				default:
					out.addFailed(err)
					if consecutive.Add(1) >= zennMaxConsecutiveFailures && down.CompareAndSwap(false, true) {
						out.truncate(msgZennDown)
					}
					continue
				}
				consecutive.Store(0)

				recs := make([]Record, 0, len(arts))
				for _, a := range arts {
					if a.PublishedAt.Before(cutoff) {
						continue
					}
					recs = append(recs, Record{Article: a, TagIDs: []int32{tag.ID}})
				}
				saved, ok := j.save(ctx, recs, db)
				if !ok {
					continue
				}
				out.addOK(saved)
				if len(arts) == 0 {
					out.mu.Lock()
					out.zero = append(out.zero, tag.ZennTopic)
					out.mu.Unlock()
				}
			}
		}()
	}
	for _, t := range tags {
		jobs <- t
	}
	close(jobs)
	wg.Wait()
}

// describe は、記録してよい説明文だけを返す。provider.Error は提供元・分類・状態コードだけを含み、
// URL・ヘッダー・本文を含まない。それ以外のエラー(DBの生エラーなど)は固定の文に置き換える。
func describe(err error) string {
	var pe *provider.Error
	switch {
	case errors.As(err, &pe):
		return safeText(pe.Error())
	case errors.Is(err, errCallLimit):
		return msgCallLimit
	case errors.Is(err, context.DeadlineExceeded):
		return msgTimeout
	case errors.Is(err, context.Canceled):
		return msgCanceled
	}
	return msgFetchFailed
}

// safeText は、万一URLや認証の文字列が混ざっていたら固定の文に置き換える。
func safeText(s string) string {
	for _, bad := range []string{"://", "Bearer", "Authorization", "token="} {
		if strings.Contains(s, bad) {
			return msgFetchFailed
		}
	}
	return s
}

func joinMessages(msgs []string) string {
	s := strings.Join(msgs, " / ")
	if utf8.RuneCountInString(s) <= maxMessageRunes {
		return s
	}
	r := []rune(s)
	return string(r[:maxMessageRunes])
}
