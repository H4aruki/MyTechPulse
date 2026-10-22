package recommendation

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/H4aruki/MyTechPulse/server/internal/article"
	"github.com/H4aruki/MyTechPulse/server/internal/interest"
	"github.com/H4aruki/MyTechPulse/server/internal/provider"
)

var ErrAllProvidersFailed = errors.New("recommendation: all providers failed")

type InterestRepository interface {
	List(context.Context, int64) ([]interest.Weight, error)
	UpdateForClick(context.Context, int64, []string) ([]interest.Weight, error)
}
type ProviderSet struct {
	Qiita provider.Client
	Zenn  provider.Client
}
type Clock interface{ Now() time.Time }

// ArticleRepository は、定期一括取得(articlefetch)で貯めた記事を読む(Issue #66 段取り2)。
// source は article.SourceQiita か article.SourceZenn。返す記事には、記事に付いた全タグを入れる。
type ArticleRepository interface {
	// ListTagged は、tags のどれかが付いた(interest.NormalizeTag と同じ比べ方) since 以降の記事を、新しい順に最大 limit 件返す。
	ListTagged(ctx context.Context, source string, tags []string, since time.Time, limit int) ([]article.Article, error)
	// ListPopular は、タグを問わず since 以降の記事を、いいね数の多い順に最大 limit 件返す。
	ListPopular(ctx context.Context, source string, since time.Time, limit int) ([]article.Article, error)
}

type Service struct {
	Interests InterestRepository
	Providers ProviderSet
	// Articles があれば、フィードはDBから読み、外部APIを呼ばない。無ければ従来の即時検索を使う。
	Articles    ArticleRepository
	Clock       Clock
	FeedTimeout time.Duration
}
type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

type searchResult struct {
	provider     string
	requestedTag string
	popular      bool
	items        []article.Article
	err          error
}

// 記事を絞る期間。Qiitaは直近5日、Zennは直近14日(新しい記事が無ければ、タグで見つけた古い記事を使う)。
const (
	qiitaWindow = 5 * 24 * time.Hour
	zennWindow  = 14 * 24 * time.Hour
	// DBに残るZennの記事(公開から30日)。14日以内に無いときの古い記事は、この中から探す。
	zennStoredWindow = 30 * 24 * time.Hour
)

// DBから読む候補の上限。提供元ごとにタグで見つけた記事は新しい順に最大 dbTaggedLimit 件、
// 人気の記事は最大 dbPopularLimit 件。順位を付けて10件に絞る前の候補なので、十分に多くとる。
// 人気の記事は、すでに選んだ記事(10件未満)と重なる分を除いても、空いた枠を埋められる件数にする。
const (
	dbTaggedLimit  = 500
	dbPopularLimit = 20
)

// Get retrieves the top five interest tags concurrently, then filters, merges, scores, and limits each feed.
// 即時検索(Articles が無いとき)では、興味度が0より大きいタグが5つに満たないときだけ、提供元ごとに1回、
// タグを問わない人気の記事も取り、タグで見つけた記事と混ぜて候補にする(#240)。5つあるときは人気の記事を取らない。
// DBから読むとき(Articles があるとき)は、タグの数を問わず、提供元ごとにタグで見つけた記事が10件に満たなければ、
// 空いた枠だけを人気の記事で埋める(getFromDB)。
func (s Service) Get(ctx context.Context, userID int64) (Result, error) {
	weights, err := s.Interests.List(ctx, userID)
	if err != nil {
		return Result{}, err
	}
	if err := interest.ValidateCurrent(weights); err != nil {
		return Result{}, err
	}
	ordered := interest.Top(weights, interest.TopTagLimit)
	fill := len(ordered) < interest.TopTagLimit
	if s.Clock == nil {
		s.Clock = systemClock{}
	}
	limit := s.FeedTimeout
	if limit <= 0 {
		limit = 8 * time.Second
	}
	now := s.Clock.Now()
	requestCtx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	if s.Articles != nil {
		return s.getFromDB(requestCtx, weights, ordered, now)
	}
	targets := []struct {
		name   string
		client provider.Client
		since  time.Time
	}{{"qiita", s.Providers.Qiita, now.Add(-qiitaWindow)}, {"zenn", s.Providers.Zenn, now.Add(-zennWindow)}}
	expected := len(ordered) * len(targets)
	if fill {
		expected += len(targets)
	}
	results := make(chan searchResult, expected)
	for _, target := range targets {
		for _, weight := range ordered {
			if target.client == nil {
				results <- searchResult{provider: target.name, requestedTag: weight.Tag, err: errors.New("provider unavailable")}
				continue
			}
			go func(name, tag string, client provider.Client) {
				items, err := client.Search(requestCtx, tag)
				results <- searchResult{provider: name, requestedTag: tag, items: items, err: err}
			}(target.name, weight.Tag, target.client)
		}
		if !fill {
			continue
		}
		if target.client == nil {
			results <- searchResult{provider: target.name, popular: true, err: errors.New("provider unavailable")}
			continue
		}
		go func(name string, client provider.Client, since time.Time) {
			items, err := client.Popular(requestCtx, since)
			results <- searchResult{provider: name, popular: true, items: items, err: err}
		}(target.name, target.client, target.since)
	}
	collected := make([]searchResult, 0, expected)
	for range expected {
		collected = append(collected, <-results)
	}
	result := Result{QiitaArticles: []article.Article{}, ZennArticles: []article.Article{}, Warnings: []Warning{}}
	failed := map[string]bool{}
	succeeded := map[string]bool{}
	qiitaRaw, zennFresh, zennOld, zennPopular := []article.Article{}, []article.Article{}, []article.Article{}, []article.Article{}
	for _, batch := range collected {
		if batch.err != nil {
			failed[batch.provider] = true
			continue
		}
		succeeded[batch.provider] = true
		for _, item := range batch.items {
			validated, e := article.Validate(item)
			if e != nil {
				failed[batch.provider] = true
				succeeded[batch.provider] = false
				continue
			}
			item = validated
			if batch.provider == "qiita" {
				if !item.PublishedAt.Before(now.Add(-qiitaWindow)) {
					qiitaRaw = append(qiitaRaw, item)
				}
				continue
			}
			fresh := !item.PublishedAt.Before(now.Add(-zennWindow))
			switch {
			case batch.popular:
				// 人気の記事は、古い記事での埋め合わせに使わない
				if fresh {
					zennPopular = append(zennPopular, item)
				}
			case fresh:
				item.Tags = mergeTags(item.Tags, []string{batch.requestedTag})
				zennFresh = append(zennFresh, item)
			default:
				item.Tags = mergeTags(item.Tags, []string{batch.requestedTag})
				zennOld = append(zennOld, item)
			}
		}
	}
	for _, name := range []string{"qiita", "zenn"} {
		if failed[name] {
			result.Warnings = append(result.Warnings, Warning{Provider: name, Code: "provider_unavailable"})
		}
	}
	if failed["qiita"] && !succeeded["qiita"] && failed["zenn"] && !succeeded["zenn"] {
		return Result{}, ErrAllProvidersFailed
	}
	result.QiitaArticles = rankLimit(weights, dedupe(qiitaRaw), 10)
	// 古い記事での埋め合わせは、タグで見つけた記事が14日以内に1件も無いときだけ(人気の記事の有無は問わない)
	if len(zennFresh) == 0 {
		zennFresh = zennOld
	}
	result.ZennArticles = rankLimit(weights, dedupe(append(zennFresh, zennPopular...)), 10)
	return result, nil
}

// getFromDB は、定期一括取得で貯めた記事から候補を読む。外部APIは呼ばず、即時検索へも切り替えない。
// 期間・古い記事での埋め合わせ・順位付けは、即時検索と同じ。人気の記事の扱いだけが違い、
// 提供元ごとに、タグで見つけた記事が10件に満たないときは、タグの数を問わず、空いた枠を人気の記事で埋める。
// タグで見つけた記事を必ず先に並べ、人気の記事はその後ろに足す。10件そろった提供元では人気の記事を読まない。
// DBには取得の時点の記事が残っているため、取得が失敗しても、残っている記事を出す。
func (s Service) getFromDB(ctx context.Context, weights []interest.Weight, ordered []interest.Weight, now time.Time) (Result, error) {
	tags := make([]string, 0, len(ordered))
	for _, weight := range ordered {
		tags = append(tags, weight.Tag)
	}
	qiitaSince, zennSince := now.Add(-qiitaWindow), now.Add(-zennWindow)
	qiitaRaw, zennFresh, zennOld := []article.Article{}, []article.Article{}, []article.Article{}
	if len(tags) > 0 {
		items, err := s.Articles.ListTagged(ctx, article.SourceQiita, tags, qiitaSince, dbTaggedLimit)
		if err != nil {
			return Result{}, err
		}
		qiitaRaw = validArticlesSince(items, qiitaSince)
		items, err = s.Articles.ListTagged(ctx, article.SourceZenn, tags, now.Add(-zennStoredWindow), dbTaggedLimit)
		if err != nil {
			return Result{}, err
		}
		for _, item := range validArticlesSince(items, time.Time{}) {
			if item.PublishedAt.Before(zennSince) {
				zennOld = append(zennOld, item)
			} else {
				zennFresh = append(zennFresh, item)
			}
		}
	}
	// 古い記事での埋め合わせは、タグで見つけた記事が14日以内に1件も無いときだけ(人気の記事の有無は問わない)
	if len(zennFresh) == 0 {
		zennFresh = zennOld
	}
	qiita, err := s.fillWithPopular(ctx, weights, rankLimit(weights, dedupe(qiitaRaw), 10), article.SourceQiita, qiitaSince)
	if err != nil {
		return Result{}, err
	}
	// 人気の記事は14日以内だけで、古い記事での埋め合わせに使わない
	zenn, err := s.fillWithPopular(ctx, weights, rankLimit(weights, dedupe(zennFresh), 10), article.SourceZenn, zennSince)
	if err != nil {
		return Result{}, err
	}
	return Result{QiitaArticles: qiita, ZennArticles: zenn, Warnings: []Warning{}}, nil
}

// fillWithPopular は、ranked が10件に満たないとき、source の since 以降の人気の記事で空いた枠を埋める。
// ranked にある記事(URLが同じもの)は除き、残りを ScoreArticles と同じ順に並べて ranked の後ろに足す。
// 除くのは多くても ranked の9件なので、dbPopularLimit(20)件読めば、空いた枠を埋めるのに足りる。
func (s Service) fillWithPopular(ctx context.Context, weights []interest.Weight, ranked []article.Article, source string, since time.Time) ([]article.Article, error) {
	if len(ranked) >= 10 {
		return ranked, nil
	}
	items, err := s.Articles.ListPopular(ctx, source, since, dbPopularLimit)
	if err != nil {
		return nil, err
	}
	chosen := make(map[string]bool, len(ranked))
	for _, item := range ranked {
		chosen[item.URL] = true
	}
	popular := make([]article.Article, 0, len(items))
	for _, item := range validArticlesSince(items, since) {
		if !chosen[item.URL] {
			popular = append(popular, item)
		}
	}
	return append(ranked, rankLimit(weights, dedupe(popular), 10-len(ranked))...), nil
}

// validArticlesSince は、article.Validate を通り、since 以降に公開された記事だけを返す。不正な記事は飛ばす。
func validArticlesSince(items []article.Article, since time.Time) []article.Article {
	out := make([]article.Article, 0, len(items))
	for _, item := range items {
		validated, err := article.Validate(item)
		if err != nil || validated.PublishedAt.Before(since) {
			continue
		}
		out = append(out, validated)
	}
	return out
}

func (s Service) RecordClick(ctx context.Context, userID int64, tags []string) error {
	_, err := s.Interests.UpdateForClick(ctx, userID, tags)
	return err
}

func mergeTags(a, b []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(a)+len(b))
	for _, list := range [][]string{a, b} {
		for _, tag := range list {
			key := interest.NormalizeTag(tag)
			if key != "" && !seen[key] {
				seen[key] = true
				out = append(out, tag)
			}
		}
	}
	return out
}
func dedupe(items []article.Article) []article.Article {
	byURL := map[string]int{}
	out := make([]article.Article, 0, len(items))
	for _, item := range items {
		if i, ok := byURL[item.URL]; ok {
			out[i].Tags = mergeTags(out[i].Tags, item.Tags)
			continue
		}
		byURL[item.URL] = len(out)
		item.Tags = mergeTags(item.Tags, nil)
		out = append(out, item)
	}
	return out
}
func rankLimit(weights []interest.Weight, items []article.Article, limit int) []article.Article {
	scored := ScoreArticles(weights, items)
	if len(scored) > limit {
		scored = scored[:limit]
	}
	out := make([]article.Article, 0, len(scored))
	for _, item := range scored {
		out = append(out, item.Article)
	}
	return out
}
func warningCode(err error) string {
	if err == nil {
		return ""
	}
	return "provider_unavailable"
}

var _ = strings.TrimSpace
