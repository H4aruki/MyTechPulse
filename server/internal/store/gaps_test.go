package store

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/H4aruki/MyTechPulse/server/internal/article"
	"github.com/H4aruki/MyTechPulse/server/internal/articlefetch"
	"github.com/H4aruki/MyTechPulse/server/internal/auth"
	"github.com/H4aruki/MyTechPulse/server/internal/passkey"
)

// 接続しないプール。入力検査で返る経路と、DBへ届く前に失敗する経路に使う。
const unreachableStoreDSN = "postgres://mtp_unreachable:mtp_unreachable@127.0.0.1:1/db?sslmode=disable&connect_timeout=1"

var gapNow = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func newUnreachablePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), unreachableStoreDSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func canceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func requireExactError(t *testing.T, name string, err error, want string) {
	t.Helper()
	if err == nil || err.Error() != want {
		t.Errorf("%s: err = %v, want %q", name, err, want)
		return
	}
	if errors.Is(err, context.Canceled) {
		t.Errorf("%s: DBの原因をそのまま外へ返している", name)
	}
	for _, leak := range []string{"mtp_unreachable", "127.0.0.1"} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("%s: 接続先がエラーに含まれている: %v", name, err)
		}
	}
}

func TestFeedArticleFillsUnknownSourceAndNilTags(t *testing.T) {
	published := pgtype.Timestamptz{Time: gapNow, Valid: true}
	got := feedArticle(7, "hatena", "題", "https://example.com/a", "著者", 4, published, nil)
	if got.ID != 7 || got.Source != "hatena" || got.Title != "題" || got.Likes != 4 || !got.PublishedAt.Equal(gapNow) {
		t.Fatalf("article = %+v", got)
	}
	if got.Tags == nil {
		t.Fatal("タグが無い行は、nil ではなく空の一覧にする")
	}
	if len(got.Tags) != 0 {
		t.Fatalf("tags = %#v", got.Tags)
	}
}

func TestClampLimitCapsAtInt32(t *testing.T) {
	if got := clampLimit(math.MaxInt32 + 1); got != math.MaxInt32 {
		t.Fatalf("clampLimit(MaxInt32+1) = %d", got)
	}
	if got := clampLimit(3); got != 3 {
		t.Fatalf("clampLimit(3) = %d", got)
	}
}

func TestNormalizedTagKeysDropsBlanksAndDuplicates(t *testing.T) {
	got := normalizedTagKeys([]string{" Go ", "", "go", "Rust", " rust ", "  "})
	want := []string{"go", "rust"}
	if len(got) != len(want) {
		t.Fatalf("keys = %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("keys = %#v, want %#v", got, want)
		}
	}
	if len(normalizedTagKeys(nil)) != 0 {
		t.Fatal("nil は空")
	}
}

func TestListPopularRejectsNonPositiveLimitBeforeQuery(t *testing.T) {
	// 件数が0以下なら問い合わせない。接続プールが無くても空を返す。
	s := &FeedArticles{}
	for _, limit := range []int{0, -1} {
		got, err := s.ListPopular(context.Background(), article.SourceQiita, gapNow, limit)
		if err != nil || len(got) != 0 {
			t.Fatalf("limit %d: got = %#v, err = %v", limit, got, err)
		}
	}
}

func TestAuthCreateRejectsOutOfRangeUserIDBeforeDB(t *testing.T) {
	a := &Auth{}
	for _, id := range []int64{0, -1, math.MaxInt32 + 1} {
		err := a.Create(context.Background(), [32]byte{1}, id, gapNow)
		if !errors.Is(err, auth.ErrNotFound) {
			t.Errorf("Create(userID=%d): err = %v, want ErrNotFound", id, err)
		}
	}
}

func TestInterestRejectsOutOfRangeUserIDBeforeDB(t *testing.T) {
	r := &Interest{}
	ctx := context.Background()
	for _, id := range []int64{0, -4, math.MaxInt32 + 1} {
		if _, err := r.List(ctx, id); err == nil || err.Error() != "store: 興味度を取得できません" {
			t.Errorf("List(%d): err = %v", id, err)
		}
		if _, err := r.UpdateForClick(ctx, id, []string{"go"}); err == nil || err.Error() != "store: 興味度を更新できません" {
			t.Errorf("UpdateForClick(%d): err = %v", id, err)
		}
	}
}

func TestSavedArticlesRejectsOutOfRangeUserIDBeforeDB(t *testing.T) {
	s := &SavedArticles{}
	ctx := context.Background()
	for _, id := range []int64{0, -1, math.MaxInt32 + 1} {
		created, err := s.Save(ctx, id, 1)
		if created || !errors.Is(err, ErrSavedUserNotFound) {
			t.Errorf("Save(userID=%d): created = %v, err = %v", id, created, err)
		}
		items, total, err := s.List(ctx, id, 10, 0)
		if err != nil || total != 0 || len(items) != 0 {
			t.Errorf("List(userID=%d): items = %#v, total = %d, err = %v", id, items, total, err)
		}
	}
}

// DBへ届かない失敗は、固定の文言にまとめる。記事の一部の読み書きは、今の実装ではドライバのエラーをそのまま返す。
func TestStoreDatabaseFailuresUseFixedMessagesOrTheDriverError(t *testing.T) {
	pool := newUnreachablePool(t)
	ctx := canceledContext()
	articles := NewArticleFetch(pool)
	authRepo := NewAuth(pool)
	feed := NewFeedArticles(pool)
	feedback := NewFeedback(pool)
	interestRepo := NewInterest(pool)
	saved := NewSavedArticles(pool)
	passkeys := NewPasskey(pool)

	raw := []struct {
		name string
		err  error
	}{
		{"ArticleFetch.ListTags", func() error { _, err := articles.ListTags(ctx); return err }()},
		{"ArticleFetch.ListFetchTags", func() error { _, err := articles.ListFetchTags(ctx); return err }()},
		{"ArticleFetch.SaveArticles", func() error {
			_, err := articles.SaveArticles(ctx, []articlefetch.Record{{Article: article.Article{
				Source: article.SourceQiita, ExternalID: "e", URL: "https://example.com/e", Title: "t", PublishedAt: gapNow,
			}}})
			return err
		}()},
		{"ArticleFetch.RunsForSlot", func() error { _, err := articles.RunsForSlot(ctx, gapNow); return err }()},
	}
	for _, tc := range raw {
		if !errors.Is(tc.err, context.Canceled) {
			t.Errorf("%s: err = %v, want context.Canceled", tc.name, tc.err)
		}
	}

	const (
		msgArticleDelete = "store: 期限切れの記事を消せません"
		msgLock          = "store: ロック用の接続を作れません"
		msgUser          = "store: 利用者を取得できません"
		msgTx            = "store: トランザクションを開始できません"
		msgSessionCreate = "store: セッションを作成できません"
		msgSessionRead   = "store: セッションを取得できません"
		msgSessionDelete = "store: セッションを削除できません"
		msgSessionExpire = "store: 期限切れセッションを削除できません"
		msgFeed          = "store: フィードの記事を取得できません"
		msgFeedback      = "store: 利用者フィードバックを処理できません"
		msgInterestRead  = "store: 興味度を取得できません"
		msgInterestWrite = "store: 興味度を更新できません"
		msgUnsave        = "store: 保存を解除できません"
		msgSavedList     = "store: 保存した記事を取得できません"
	)
	promptID := "0123abcd-4567-89ab-cdef-0123456789ab"
	masked := []struct {
		name string
		err  error
		want string
	}{
		{"ArticleFetch.DeleteExpired", func() error { _, err := articles.DeleteExpired(ctx, "qiita", gapNow); return err }(), msgArticleDelete},
		{"ArticleFetch.TryLock", func() error { _, _, err := articles.TryLock(ctx); return err }(), msgLock},
		{"Auth.FindByUsername", func() error { _, err := authRepo.FindByUsername(ctx, "alice"); return err }(), msgUser},
		{"Auth.FindByID", func() error { _, err := authRepo.FindByID(ctx, 1); return err }(), msgUser},
		{"Auth.CreateWithInterestsAndSession", func() error {
			_, err := authRepo.CreateWithInterestsAndSession(ctx, "alice", "hash", auth.RoleMember, []string{"Go"}, [32]byte{1}, gapNow)
			return err
		}(), msgTx},
		{"Auth.Create", authRepo.Create(ctx, [32]byte{2}, 1, gapNow), msgSessionCreate},
		{"Auth.FindUser", func() error { _, err := authRepo.FindUser(ctx, [32]byte{2}, gapNow); return err }(), msgSessionRead},
		{"Auth.Delete", authRepo.Delete(ctx, [32]byte{2}), msgSessionDelete},
		{"Auth.DeleteExpired", authRepo.DeleteExpired(ctx, gapNow), msgSessionExpire},
		{"Feed.ListTagged", func() error {
			_, err := feed.ListTagged(ctx, article.SourceQiita, []string{"go"}, gapNow, 10)
			return err
		}(), msgFeed},
		{"Feed.ListPopular", func() error { _, err := feed.ListPopular(ctx, article.SourceQiita, gapNow, 10); return err }(), msgFeed},
		{"Feed.ListCooccurringTags", func() error {
			_, err := feed.ListCooccurringTags(ctx, []string{" Go ", "go"}, []string{"go", " go "})
			return err
		}(), msgFeed},
		{"Feedback.Present", func() error { _, err := feedback.Present(ctx, 1, promptID, gapNow); return err }(), msgFeedback},
		{"Feedback.SubmitOverall", func() error { _, err := feedback.SubmitOverall(ctx, 1, promptID, 1, 4, gapNow); return err }(), msgFeedback},
		{"Feedback.CompleteFollowup", func() error { _, err := feedback.CompleteFollowup(ctx, 1, promptID, nil, gapNow); return err }(), msgFeedback},
		{"Feedback.Dismiss", func() error { _, err := feedback.Dismiss(ctx, 1, promptID, gapNow); return err }(), msgFeedback},
		{"Feedback.DeleteExpired", func() error { _, err := feedback.DeleteExpired(ctx, gapNow, 10); return err }(), msgFeedback},
		{"Interest.List", func() error { _, err := interestRepo.List(ctx, 1); return err }(), msgInterestRead},
		{"Interest.UpdateForClick", func() error { _, err := interestRepo.UpdateForClick(ctx, 1, []string{"go"}); return err }(), msgInterestWrite},
		{"SavedArticles.Save", func() error { _, err := saved.Save(ctx, 1, 1); return err }(), msgTx},
		{"SavedArticles.Unsave", saved.Unsave(ctx, 1, 1), msgUnsave},
		{"SavedArticles.List", func() error { _, _, err := saved.List(ctx, 1, 10, 0); return err }(), msgSavedList},
		{"Passkey.Create", func() error {
			_, err := passkeys.Create(ctx, passkey.Credential{UserID: 1, CredentialID: []byte{1}, PublicKey: []byte{2}, Name: "k"})
			return err
		}(), msgTx},
	}
	for _, tc := range masked {
		requireExactError(t, tc.name, tc.err, tc.want)
	}
}
