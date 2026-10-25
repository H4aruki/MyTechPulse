// Package savedarticle は、利用者が記事を保存(お気に入り)して、あとで見返す機能。
package savedarticle

import (
	"context"

	"github.com/H4aruki/MyTechPulse/server/internal/store"
)

// 一覧の件数の既定と上限。
const (
	DefaultLimit = 20
	MaxLimit     = 100
)

// 保存まわりの誤り。store の誤りを、HTTP の層が store に依存せず判定できるようにここで包み直す。
var (
	ErrArticleNotFound = store.ErrArticleNotFound
	ErrLimitReached    = store.ErrSavedLimitReached
	ErrUserNotFound    = store.ErrSavedUserNotFound
)

// Article は保存した記事1件。
type Article = store.SavedArticle

// Repo は保存した記事の保管先。
type Repo interface {
	Save(ctx context.Context, userID, articleID int64) (created bool, err error)
	Unsave(ctx context.Context, userID, articleID int64) error
	List(ctx context.Context, userID int64, limit, offset int) (items []Article, total int, err error)
}

// Service は保存した記事の業務処理。
type Service struct {
	Repo Repo
}

// Save は記事を保存する。新しく保存したときだけ created が true。
func (s *Service) Save(ctx context.Context, userID, articleID int64) (bool, error) {
	return s.Repo.Save(ctx, userID, articleID)
}

// Unsave は保存を解除する。保存していなくても、記事が存在しなくても成功する(冪等)。
func (s *Service) Unsave(ctx context.Context, userID, articleID int64) error {
	return s.Repo.Unsave(ctx, userID, articleID)
}

// List は保存した記事を、保存の新しい順に返す。total は、ページングする前の保存の総件数。
func (s *Service) List(ctx context.Context, userID int64, limit, offset int) ([]Article, int, error) {
	items, total, err := s.Repo.List(ctx, userID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	if items == nil {
		items = []Article{}
	}
	return items, total, nil
}
