package recommendation

import (
	"math"
	"sort"

	"github.com/H4aruki/MyTechPulse/server/internal/article"
	"github.com/H4aruki/MyTechPulse/server/internal/interest"
)

// ScoreArticles computes the provider-specific score and applies deterministic tie-breaks.
// 同点のときは新しい記事、提供元名、アドレスの順。ただし点数0の記事どうしは、まずいいね数の多い順にする。
func ScoreArticles(weights []interest.Weight, articles []article.Article) []ScoredArticle {
	byTag := make(map[string]int64, len(weights))
	for _, weight := range weights {
		key := interest.NormalizeTag(weight.Tag)
		if key != "" {
			byTag[key] = weight.Value
		}
	}
	out := make([]ScoredArticle, 0, len(articles))
	for _, item := range articles {
		var total int64
		seen := make(map[string]struct{}, len(item.Tags))
		for _, tag := range item.Tags {
			key := interest.NormalizeTag(tag)
			if _, duplicate := seen[key]; duplicate {
				continue
			}
			seen[key] = struct{}{}
			if value, ok := byTag[key]; ok && value > 0 {
				if total > math.MaxInt64-value {
					total = math.MaxInt64
				} else {
					total += value
				}
			}
		}
		score := total
		if item.Source == article.SourceQiita {
			multiplier := int64(item.Likes) + 1
			if multiplier < 1 || total > math.MaxInt64/multiplier {
				score = math.MaxInt64
			} else {
				score = total * multiplier
			}
		}
		out = append(out, ScoredArticle{Article: item, Score: score})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		// 点数0の記事(興味のあるタグが1つも付いていない、空き枠を埋める人気の記事など)は、いいね数の多い順(#240)
		if out[i].Score == 0 && out[i].Article.Likes != out[j].Article.Likes {
			return out[i].Article.Likes > out[j].Article.Likes
		}
		if !out[i].Article.PublishedAt.Equal(out[j].Article.PublishedAt) {
			return out[i].Article.PublishedAt.After(out[j].Article.PublishedAt)
		}
		if out[i].Article.Source != out[j].Article.Source {
			return out[i].Article.Source < out[j].Article.Source
		}
		return out[i].Article.URL < out[j].Article.URL
	})
	return out
}
