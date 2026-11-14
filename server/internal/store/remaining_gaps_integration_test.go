package store_test

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/H4aruki/MyTechPulse/server/internal/article"
	"github.com/H4aruki/MyTechPulse/server/internal/articlefetch"
	"github.com/H4aruki/MyTechPulse/server/internal/auth"
	"github.com/H4aruki/MyTechPulse/server/internal/interest"
	"github.com/H4aruki/MyTechPulse/server/internal/passkey"
	"github.com/H4aruki/MyTechPulse/server/internal/store"
	"github.com/H4aruki/MyTechPulse/server/internal/userfeedback"
)

const gapPrompt = "44444444-4444-4444-8444-444444444444"

func readOnlyPool(t *testing.T, base *pgxpool.Pool) *pgxpool.Pool {
	t.Helper()
	cfg := base.Config().Copy()
	if cfg.ConnConfig.RuntimeParams == nil {
		cfg.ConnConfig.RuntimeParams = map[string]string{}
	}
	// 書き込みを拒否する接続。途中まで進んだ処理が、失敗したら何も残さないことを確かめる。
	cfg.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func requireStoreText(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestFeedArticlesLimitAndDuplicateSeeds(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	fetch := store.NewArticleFetch(pool)
	feed := store.NewFeedArticles(pool)
	goTag := newTag(t, pool, "Go")
	rustTag := newTag(t, pool, "Rust")
	since := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	recs := []articlefetch.Record{
		record(article.SourceQiita, "q1", "https://qiita.com/u/items/gap-q1", since.Add(time.Hour), goTag, rustTag),
	}
	if res, err := fetch.SaveArticles(ctx, recs); err != nil || res.Saved != 1 {
		t.Fatalf("res = %+v, err = %v", res, err)
	}

	// 0件以下は、記事があっても問い合わせずに空を返す
	for _, limit := range []int{0, -3} {
		got, err := feed.ListPopular(ctx, article.SourceQiita, since, limit)
		if err != nil || len(got) != 0 {
			t.Fatalf("limit %d: got = %v, err = %v", limit, feedURLs(got), err)
		}
	}

	// int32 に収まらない件数は、上限に収めて記事を返す
	got, err := feed.ListTagged(ctx, article.SourceQiita, []string{"go"}, since, math.MaxInt32+1)
	if err != nil || len(got) != 1 || got[0].URL != "https://qiita.com/u/items/gap-q1" {
		t.Fatalf("huge limit: got = %v, err = %v", feedURLs(got), err)
	}

	// 同じタグの表記ゆれは1回だけ種にする
	once, err := feed.ListCooccurringTags(ctx, []string{"go"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	dup, err := feed.ListCooccurringTags(ctx, []string{" Go ", "go", " GO "}, []string{" Go ", "go"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(dup, once) {
		t.Fatalf("重複した種 = %+v, 1回だけ = %+v", dup, once)
	}
}

func TestArticleFetchUnknownSourceIsSkippedAndReadOnlyWriteFails(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	s := store.NewArticleFetch(pool)
	recs := []articlefetch.Record{
		record("other", "bad", "https://example.com/not-a-provider", articlePublished),
		record(article.SourceQiita, "ok", "https://qiita.com/u/items/gap-ok", articlePublished),
	}
	res, err := s.SaveArticles(ctx, recs)
	if err != nil || res.Saved != 1 || res.Skipped != 1 {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM articles WHERE external_id = 'bad'`); n != 0 {
		t.Fatal("提供元が qiita/zenn 以外の記事が残っている")
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM articles WHERE external_id = 'ok'`); n != 1 {
		t.Fatal("続く記事が保存されていない")
	}

	ro := store.NewArticleFetch(readOnlyPool(t, pool))
	res, err = ro.SaveArticles(ctx, []articlefetch.Record{
		record(article.SourceQiita, "ro", "https://qiita.com/u/items/gap-ro", articlePublished),
	})
	if err == nil || res.Saved != 0 || res.Skipped != 0 {
		t.Fatalf("読み取り専用なのに保存された: res = %+v, err = %v", res, err)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM articles WHERE external_id = 'ro'`); n != 0 {
		t.Fatal("失敗した保存が残っている")
	}
	n, err := ro.DeleteExpired(ctx, "qiita", articlePublished.Add(24*time.Hour))
	requireStoreText(t, err, "store: 期限切れの記事を消せません")
	if n != 0 {
		t.Fatalf("deleted = %d", n)
	}
	if got := queryInt(t, pool, `SELECT count(*) FROM articles WHERE external_id = 'ok'`); got != 1 {
		t.Fatalf("期限切れの削除が記事を消した: %d", got)
	}
}

func TestAuthRepositoryRejectsUnknownRoleAndMissingSessionUser(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	repo := store.NewAuth(pool)
	_, err := repo.CreateWithInterestsAndSession(ctx, "alice", "hash", auth.Role("guest"), nil, hashOf(9), fixedNow.Add(time.Hour))
	requireStoreText(t, err, "store: 利用者を作成できません")
	if errors.Is(err, auth.ErrUsernameTaken) {
		t.Fatal("役割の不正を、利用者名の重複と取り違えている")
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM "user"`); n != 0 {
		t.Fatalf("user = %d", n)
	}

	err = repo.Create(ctx, hashOf(8), 999999, fixedNow.Add(time.Hour))
	requireStoreText(t, err, "store: セッションを作成できません")
	if errors.Is(err, auth.ErrNotFound) {
		t.Fatal("存在しない利用者へのセッション作成を ErrNotFound にしている")
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM auth_session`); n != 0 {
		t.Fatalf("session = %d", n)
	}
}

func TestPasskeyCreateStoresEmptyTransportsAndRejectsEmptyPublicKey(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	repo := store.NewPasskey(pool)
	alice := newPasskeyUser(t, pool, "alice")

	c := credential(alice, 1)
	c.Transports = nil
	created, err := repo.Create(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	if created.Transports == nil || len(created.Transports) != 0 {
		t.Fatalf("Transports = %#v, want empty", created.Transports)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM auth_passkey WHERE transports = '{}'::text[]`); n != 1 {
		t.Fatalf("empty transports rows = %d", n)
	}

	empty := credential(alice, 2)
	empty.PublicKey = []byte{}
	_, err = repo.Create(ctx, empty)
	if err == nil || err.Error() != "store: パスキーを登録できません" {
		t.Fatalf("empty public key: err = %v", err)
	}
	if errors.Is(err, passkey.ErrCredentialExists) || errors.Is(err, passkey.ErrNotFound) || errors.Is(err, passkey.ErrLimitReached) {
		t.Fatalf("公開鍵が空なのに、登録済みや上限のエラーになっている: %v", err)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM auth_passkey`); n != 1 {
		t.Fatalf("passkeys = %d", n)
	}
}

func TestInterestDueLearningRollsBackWhenTagsCollide(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	saved := store.NewSavedArticles(pool)
	in := store.NewInterest(pool)
	user := newPasskeyUser(t, pool, "alice")
	ids := newSavableArticles(t, pool, "collide", 1)
	setWeight(t, pool, user, "Go", 5000)
	if _, err := pool.Exec(ctx, `INSERT INTO tag(tag_name) VALUES (' go ')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO recommend("user_ID","tag_ID",match_int) SELECT $1,"tag_ID",1725 FROM tag WHERE tag_name=' go '`, user); err != nil {
		t.Fatal(err)
	}
	tagArticle(t, pool, ids[0], "Go")
	mustSave(t, saved, user, ids[0])
	backdateSaves(t, pool, user)

	if _, err := in.List(ctx, user); !errors.Is(err, interest.ErrNormalizedTagCollision) {
		t.Fatalf("List = %v", err)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM saved_articles WHERE learned_at IS NOT NULL`); n != 0 {
		t.Fatal("衝突で失敗した学習が反映済みになっている")
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM recommend WHERE "user_ID"=$1 AND match_int IN (5000, 1725)`, user); n != 2 {
		t.Fatalf("興味度の行が変わった: %d", n)
	}
}

func TestReadOnlyStoreWritesFailWithoutChangingRows(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	user := newPasskeyUser(t, pool, "alice")
	ids := newSavableArticles(t, pool, "ro", 2)
	mustSave(t, store.NewSavedArticles(pool), user, ids[0])
	backdateSaves(t, pool, user)
	feedbackRepo := store.NewFeedback(pool)
	if _, err := feedbackRepo.Present(ctx, user, promptA, fixedNow); err != nil {
		t.Fatal(err)
	}

	ro := readOnlyPool(t, pool)
	saved := store.NewSavedArticles(ro)
	created, err := saved.Save(ctx, user, ids[1])
	requireStoreText(t, err, "store: 記事を保存できません")
	if created || errors.Is(err, store.ErrSavedUserNotFound) || errors.Is(err, store.ErrArticleNotFound) {
		t.Fatalf("created = %v, err = %v", created, err)
	}

	if _, err := store.NewPasskey(ro).Create(ctx, credential(user, 3)); err == nil || err.Error() != "store: パスキーを登録できません" {
		t.Fatalf("passkey: err = %v", err)
	}
	if _, err := store.NewInterest(ro).UpdateForClick(ctx, user, []string{"go"}); err == nil || err.Error() != "store: 興味度を更新できません" {
		t.Fatalf("click: err = %v", err)
	}
	if _, err := store.NewInterest(ro).List(ctx, user); err == nil || err.Error() != "store: 興味度を更新できません" {
		t.Fatalf("list due: err = %v", err)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM saved_articles WHERE learned_at IS NOT NULL`); n != 0 {
		t.Fatal("読み取り専用の参照で学習が反映された")
	}

	roFeedback := store.NewFeedback(ro)
	if _, err := roFeedback.Present(ctx, user, gapPrompt, fixedNow); err == nil || err.Error() != "store: 利用者フィードバックを処理できません" {
		t.Fatalf("present: err = %v", err)
	}
	if _, err := roFeedback.Dismiss(ctx, user, promptA, fixedNow); err == nil || err.Error() != "store: 利用者フィードバックを処理できません" {
		t.Fatalf("dismiss: err = %v", err)
	}
	if n, err := roFeedback.DeleteExpired(ctx, fixedNow.Add(24*time.Hour), 10); err == nil || err.Error() != "store: 利用者フィードバックを処理できません" || n != 0 {
		t.Fatalf("delete expired: n = %d, err = %v", n, err)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM feedback_prompts WHERE status = 'shown'`); n != 1 {
		t.Fatalf("shown prompts = %d", n)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM auth_passkey`); n != 0 {
		t.Fatalf("passkeys = %d", n)
	}
}

func TestFeedbackPromptEdgesAndBrokenForms(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	repo := store.NewFeedback(pool)
	uid := feedbackUser(t, pool, "fb-gap", 71)

	last, err := repo.LastPrompt(ctx, uid, "service_satisfaction")
	if err != nil || last != nil {
		t.Fatalf("履歴が無い: last = %+v, err = %v", last, err)
	}
	if _, err := repo.SubmitOverall(ctx, uid, gapPrompt, 1, 4, fixedNow); !errors.Is(err, userfeedback.ErrNotFound) {
		t.Fatalf("未知の表示: err = %v", err)
	}

	form := presentForTest(t, repo, uid, promptA)
	if _, err := repo.Dismiss(ctx, uid, promptA, fixedNow); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SubmitOverall(ctx, uid, promptA, form.Root().ID, 4, fixedNow); !errors.Is(err, userfeedback.ErrConflict) {
		t.Fatalf("閉じたあとの回答: err = %v", err)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM feedback_submissions`); n != 0 {
		t.Fatalf("submissions = %d", n)
	}

	// 閉じた表示のあとも、再表示までの間隔が明けるまで次の表示は出ない
	shownAt := fixedNow.AddDate(0, 0, 60)
	presented, err := repo.Present(ctx, uid, promptB, shownAt)
	if err != nil || presented.Form == nil {
		t.Fatalf("present = %+v, %v", presented, err)
	}
	form = *presented.Form
	got, err := repo.SubmitOverall(ctx, uid, promptB, form.Root().ID, 4, shownAt.Add(time.Minute))
	if err != nil || got.Status != userfeedback.SubmissionCompleted {
		t.Fatalf("submit = %+v, %v", got, err)
	}
	last, err = repo.LastPrompt(ctx, uid, "service_satisfaction")
	if err != nil || last == nil || last.SubmissionID == nil || *last.SubmissionID != got.SubmissionID || last.SubmissionStatus == nil || *last.SubmissionStatus != userfeedback.SubmissionCompleted {
		t.Fatalf("last = %+v, err = %v", last, err)
	}
	replay, err := repo.Present(ctx, uid, promptB, fixedNow.Add(2*time.Minute))
	if err != nil || !replay.Eligible || replay.Form != nil || replay.Prompt == nil || replay.Prompt.Status != userfeedback.PromptSubmitted ||
		replay.Prompt.SubmissionID == nil || *replay.Prompt.SubmissionID != got.SubmissionID {
		t.Fatalf("replay = %+v, err = %v", replay, err)
	}

	if _, err := pool.Exec(ctx, `UPDATE feedback_forms SET status = 'retired' WHERE status = 'active'`); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ActiveForm(ctx); !errors.Is(err, userfeedback.ErrNotFound) {
		t.Fatalf("有効なフォームが無い: err = %v", err)
	}
	blocked, err := repo.Present(ctx, uid, promptC, fixedNow.Add(time.Hour))
	if err != nil || blocked.Eligible || blocked.Prompt != nil {
		t.Fatalf("フォームが無いときの表示 = %+v, %v", blocked, err)
	}

	if _, err := pool.Exec(ctx, `UPDATE feedback_forms SET status = 'active' WHERE form_key = 'service_satisfaction' AND version = 1`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO feedback_questions (form_id, question_key, question_text, sort_order, is_required)
		SELECT id, 'extra_root', 'もう一つの総合評価', 50, true FROM feedback_forms WHERE status = 'active'`); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ActiveForm(ctx); !errors.Is(err, userfeedback.ErrInvalidDefinition) {
		t.Fatalf("壊れたフォーム: err = %v", err)
	}
	promptsBefore := queryInt(t, pool, `SELECT count(*) FROM feedback_prompts`)
	if _, err := repo.Present(ctx, uid, gapPrompt, fixedNow.Add(2*time.Hour)); !errors.Is(err, userfeedback.ErrInvalidDefinition) {
		t.Fatalf("壊れたフォームの表示: err = %v", err)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM feedback_prompts`); n != promptsBefore {
		t.Fatalf("prompts = %d, want %d", n, promptsBefore)
	}
}
