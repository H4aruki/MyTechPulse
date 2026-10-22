package articlefetch

import (
	"context"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/H4aruki/MyTechPulse/server/internal/article"
	"github.com/H4aruki/MyTechPulse/server/internal/provider/qiita"
)

var jst = time.FixedZone("JST", 9*60*60)

func jstTime(y int, m time.Month, d, h, min int) time.Time {
	return time.Date(y, m, d, h, min, 0, 0, jst)
}

// fakeClock は手動で進める時計。auto が true なら、After は待たずに（待つ長さを記録して）すぐ返る。
type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	auto    bool
	waiters []*waiter
	waits   []time.Duration
}

type waiter struct {
	at time.Time
	ch chan time.Time
}

func newFakeClock(now time.Time) *fakeClock { return &fakeClock{now: now} }

func newAutoClock(now time.Time) *fakeClock { return &fakeClock{now: now, auto: true} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.waits = append(c.waits, d)
	ch := make(chan time.Time, 1)
	if c.auto || d <= 0 {
		ch <- c.now
		return ch
	}
	c.waiters = append(c.waiters, &waiter{at: c.now.Add(d), ch: ch})
	return ch
}

func (c *fakeClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

func (c *fakeClock) Waits() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Duration(nil), c.waits...)
}

// nextWaiter は、いちばん早い待ちの時刻。待ちが無ければ false。
func (c *fakeClock) nextWaiter() (time.Time, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var best time.Time
	for _, w := range c.waiters {
		if best.IsZero() || w.at.Before(best) {
			best = w.at
		}
	}
	return best, !best.IsZero()
}

// fire は、時刻 t までに期限が来た待ちを起こし、時計を t にする。
func (c *fakeClock) fire(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if t.After(c.now) {
		c.now = t
	}
	rest := c.waiters[:0]
	for _, w := range c.waiters {
		if !w.at.After(c.now) {
			w.ch <- c.now
		} else {
			rest = append(rest, w)
		}
	}
	c.waiters = rest
}

// advanceTo は、時計を target まで進める。待ちの期限ごとに止まり、起こされたゴルーチンが次の待ちを登録するのを待つ。
func (c *fakeClock) advanceTo(t *testing.T, target time.Time) {
	t.Helper()
	for i := 0; i < 1000; i++ {
		deadline := time.Now().Add(2 * time.Second)
		var next time.Time
		var ok bool
		for time.Now().Before(deadline) {
			if next, ok = c.nextWaiter(); ok {
				break
			}
			time.Sleep(time.Millisecond)
		}
		if !ok {
			t.Fatal("待ちが登録されない")
		}
		if next.After(target) {
			c.Set(target)
			return
		}
		c.fire(next)
		if !c.Now().Before(target) {
			return
		}
	}
	t.Fatal("進めすぎ")
}

// waitFor は cond が true になるまで待つ。
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("待っても満たされない: %s", what)
}

// fakeJobStore は JobStore のメモリ上の偽物。
type fakeJobStore struct {
	mu        sync.Mutex
	tags      []TagRef
	fetchTags []FetchTag
	saved     []Record
	saveErr   error
	listErr   error
	deletes   map[string]time.Time
	deleted   int64
}

func (s *fakeJobStore) ListTags(context.Context) ([]TagRef, error) { return s.tags, s.listErr }
func (s *fakeJobStore) ListFetchTags(context.Context) ([]FetchTag, error) {
	return s.fetchTags, s.listErr
}

func (s *fakeJobStore) SaveArticles(_ context.Context, recs []Record) (SaveResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.saveErr != nil {
		return SaveResult{}, s.saveErr
	}
	s.saved = append(s.saved, recs...)
	return SaveResult{Saved: len(recs)}, nil
}

func (s *fakeJobStore) DeleteExpired(_ context.Context, source string, before time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.deletes == nil {
		s.deletes = map[string]time.Time{}
	}
	s.deletes[source] = before
	return s.deleted, nil
}

func (s *fakeJobStore) savedURLs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var urls []string
	for _, r := range s.saved {
		urls = append(urls, r.Article.URL)
	}
	sort.Strings(urls)
	return urls
}

// fakeQiita は QiitaSource の偽物。fn が日とページから応答を決める。
type fakeQiita struct {
	perPage int
	fn      func(day time.Time, page int) (qiita.Page, error)
	mu      sync.Mutex
	calls   []string
}

func (f *fakeQiita) ListCreated(_ context.Context, day time.Time, page int) (qiita.Page, error) {
	f.mu.Lock()
	f.calls = append(f.calls, day.Format("2006-01-02")+"#"+strconv.Itoa(page))
	f.mu.Unlock()
	return f.fn(day, page)
}

func (f *fakeQiita) FullPage(p qiita.Page) bool { return len(p.Articles) >= f.perPage }

func (f *fakeQiita) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// fakeZenn は ZennSource の偽物。
type fakeZenn struct {
	fn    func(topic string) ([]article.Article, error)
	mu    sync.Mutex
	calls []string
}

func (f *fakeZenn) SearchTopic(_ context.Context, topic string) ([]article.Article, error) {
	f.mu.Lock()
	f.calls = append(f.calls, topic)
	f.mu.Unlock()
	return f.fn(topic)
}

func (f *fakeZenn) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func qArticle(id string, tags ...string) article.Article {
	return article.Article{
		Source: article.SourceQiita, ExternalID: id, Author: "u", Title: "t " + id,
		URL: "https://qiita.com/u/items/" + id, Tags: tags, Likes: 1,
		PublishedAt: time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC),
	}
}

func zArticle(slug string) article.Article {
	return article.Article{
		Source: article.SourceZenn, ExternalID: slug, Author: "u", Title: "t " + slug,
		URL: "https://zenn.dev/u/articles/" + slug, Likes: 1,
		PublishedAt: time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC),
	}
}
