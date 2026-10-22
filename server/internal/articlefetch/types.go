package articlefetch

import (
	"context"
	"time"

	"github.com/H4aruki/MyTechPulse/server/internal/article"
	"github.com/H4aruki/MyTechPulse/server/internal/provider/qiita"
)

// 実行の状態。fetch_runs.status と同じ値。
const (
	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusPartial   = "partial"
	StatusFailed    = "failed"
)

// 実行のきっかけ。fetch_runs.trigger と同じ値。
const (
	TriggerScheduled = "scheduled"
	TriggerCatchup   = "catchup"
	TriggerRetry     = "retry"
)

// Clock は現在時刻と待ちを差し替えるための窓口。
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

// SystemClock は実際の時計。
type SystemClock struct{}

func (SystemClock) Now() time.Time                         { return time.Now() }
func (SystemClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// QiitaSource はQiitaの日ごとの新着一覧。*qiita.Client が満たす。
type QiitaSource interface {
	ListCreated(ctx context.Context, day time.Time, page int) (qiita.Page, error)
	FullPage(p qiita.Page) bool
}

// ZennSource はZennのトピック別の一覧。zenn.Client が満たす。
type ZennSource interface {
	SearchTopic(ctx context.Context, topic string) ([]article.Article, error)
}

// TagRef は tag 表の1行。
type TagRef struct {
	ID   int32
	Name string
}

// FetchTag は取得対象のタグ。ZennTopic はZennに問い合わせるトピック名。
type FetchTag struct {
	ID        int32
	Name      string
	ZennTopic string
}

// Record は保存する1記事と、結ぶ tag の ID。TagIDs には tag 表にあるタグだけを入れる。
type Record struct {
	Article article.Article
	TagIDs  []int32
}

// SaveResult は SaveArticles の結果。
type SaveResult struct {
	Saved   int // 保存（新規または更新）した記事の数
	Skipped int // 同じURLが別の記事IDで既にあるなどの理由で飛ばした記事の数
}

// JobStore は1回の取得が使うDB操作。
type JobStore interface {
	ListTags(ctx context.Context) ([]TagRef, error)
	ListFetchTags(ctx context.Context) ([]FetchTag, error)
	SaveArticles(ctx context.Context, records []Record) (SaveResult, error)
	// DeleteExpired は source（"qiita" か "zenn"）の、公開日時が before より前の記事を消し、消した数を返す。
	DeleteExpired(ctx context.Context, source string, before time.Time) (int64, error)
}

// Result は1回の取得の結果。Message には分類済みの文だけが入る（URL・トークン・応答本文は入らない）。
type Result struct {
	Status        string
	Message       string
	QiitaRequests int
	ZennRequests  int
	Upserted      int
	Deleted       int
}

// Executor は1回の取得を行う。
type Executor interface {
	Execute(ctx context.Context) Result
}

// RunRow は fetch_runs の1行(定時ごとの履歴の判断に使う分)。
type RunRow struct {
	ID         int64
	Attempt    int
	Status     string
	FinishedAt time.Time // 終わっていなければゼロ値
}

// RunStore は実行履歴のDB操作。
type RunStore interface {
	StartRun(ctx context.Context, slot time.Time, trigger string, attempt int, startedAt time.Time) (int64, error)
	FinishRun(ctx context.Context, id int64, finishedAt time.Time, res Result) error
	// RunsForSlot は slot の履歴を、新しい順に返す。
	RunsForSlot(ctx context.Context, slot time.Time) ([]RunRow, error)
	// AbandonStaleRuns は 'running' のまま残った行を失敗にする。ロックを持っているときだけ呼ぶ。
	AbandonStaleRuns(ctx context.Context, finishedAt time.Time) error
}

// Locker は複数のAPIが同時に取得しないための排他。
type Locker interface {
	// TryLock はロックを試す。取れなければ ok=false。取れたら、必ず unlock を呼ぶ。
	TryLock(ctx context.Context) (unlock func(), ok bool, err error)
}
