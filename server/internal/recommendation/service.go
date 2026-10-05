package recommendation

import (
	"context"
	"errors"
	"sort"
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
type Service struct {
	Interests   InterestRepository
	Providers   ProviderSet
	Clock       Clock
	FeedTimeout time.Duration
}
type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

type searchResult struct {
	provider     string
	requestedTag string
	items        []article.Article
	err          error
}

// Get retrieves the top five interest tags concurrently, then filters, merges, scores, and limits each feed.
func (s Service) Get(ctx context.Context, userID int64) (Result, error) {
	weights, err := s.Interests.List(ctx, userID)
	if err != nil {
		return Result{}, err
	}
	if err := interest.ValidateCurrent(weights); err != nil {
		return Result{}, err
	}
	ordered := append([]interest.Weight(nil), weights...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Value != ordered[j].Value {
			return ordered[i].Value > ordered[j].Value
		}
		return interest.NormalizeTag(ordered[i].Tag) < interest.NormalizeTag(ordered[j].Tag)
	})
	if len(ordered) > 5 {
		ordered = ordered[:5]
	}
	if s.Clock == nil {
		s.Clock = systemClock{}
	}
	limit := s.FeedTimeout
	if limit <= 0 {
		limit = 8 * time.Second
	}
	requestCtx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	results := make(chan searchResult, len(ordered)*2)
	for _, weight := range ordered {
		for _, target := range []struct {
			name   string
			client provider.Client
		}{{"qiita", s.Providers.Qiita}, {"zenn", s.Providers.Zenn}} {
			if target.client == nil {
				results <- searchResult{provider: target.name, requestedTag: weight.Tag, err: errors.New("provider unavailable")}
				continue
			}
			go func(name, tag string, client provider.Client) {
				items, err := client.Search(requestCtx, tag)
				results <- searchResult{provider: name, requestedTag: tag, items: items, err: err}
			}(target.name, weight.Tag, target.client)
		}
	}
	collected := make([]searchResult, 0, len(ordered)*2)
	for range len(ordered) * 2 {
		collected = append(collected, <-results)
	}
	result := Result{QiitaArticles: []article.Article{}, ZennArticles: []article.Article{}, Warnings: []Warning{}}
	failed := map[string]bool{}
	succeeded := map[string]bool{}
	qiitaRaw, zennFresh, zennOld := []article.Article{}, []article.Article{}, []article.Article{}
	now := s.Clock.Now()
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
				if !item.PublishedAt.Before(now.Add(-5 * 24 * time.Hour)) {
					qiitaRaw = append(qiitaRaw, item)
				}
				continue
			}
			item.Tags = mergeTags(item.Tags, []string{batch.requestedTag})
			if !item.PublishedAt.Before(now.Add(-14 * 24 * time.Hour)) {
				zennFresh = append(zennFresh, item)
			} else {
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
	if len(zennFresh) == 0 {
		zennFresh = zennOld
	}
	result.ZennArticles = rankLimit(weights, dedupe(zennFresh), 10)
	return result, nil
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
