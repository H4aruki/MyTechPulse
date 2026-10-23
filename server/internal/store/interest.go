package store

import (
	"context"
	"errors"
	"math"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/H4aruki/MyTechPulse/server/internal/interest"
	"github.com/H4aruki/MyTechPulse/server/internal/store/dbgen"
)

type Interest struct {
	pool *pgxpool.Pool
	q    *dbgen.Queries
}

var _ interface {
	List(context.Context, int64) ([]interest.Weight, error)
	UpdateForClick(context.Context, int64, []string) ([]interest.Weight, error)
} = (*Interest)(nil)

func NewInterest(pool *pgxpool.Pool) *Interest { return &Interest{pool: pool, q: dbgen.New(pool)} }

func (r *Interest) List(ctx context.Context, userID int64) ([]interest.Weight, error) {
	id, err := userID32(userID)
	if err != nil {
		return nil, errors.New("store: 興味度を取得できません")
	}
	// 期限が来た未反映の保存があるときだけ、利用者の行をロックして反映する（ふだんはロックも書き込みもしない）。
	due, err := r.q.HasDueSavedLearning(ctx, dbgen.HasDueSavedLearningParams{UserID: id, DelaySeconds: SaveLearnDelay.Seconds()})
	if err != nil {
		return nil, errors.New("store: 興味度を取得できません")
	}
	if due {
		if err := r.applySaveLearning(ctx, id); err != nil {
			return nil, err
		}
	}
	rows, err := r.q.ListRecommendations(ctx, id)
	if err != nil {
		return nil, errors.New("store: 興味度を取得できません")
	}
	out := make([]interest.Weight, 0, len(rows))
	for _, row := range rows {
		out = append(out, interest.Weight{TagID: int64(row.TagID), Tag: row.TagName, Value: int64(row.MatchInt)})
	}
	if err := interest.ValidateCurrent(out); err != nil {
		return nil, err
	}
	return out, nil
}

// applySaveLearning は、利用者の行をロックして、期限が来た保存の学習を反映する。
func (r *Interest) applySaveLearning(ctx context.Context, id int32) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return errors.New("store: 興味度を更新できません")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := r.q.WithTx(tx)
	if _, err := q.LockUserForRecommendation(ctx, id); err != nil {
		return errors.New("store: 興味度を更新できません")
	}
	if err := applyDueSaveLearning(ctx, q, id); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return errors.New("store: 興味度を更新できません")
	}
	return nil
}

func (r *Interest) UpdateForClick(ctx context.Context, userID int64, tags []string) ([]interest.Weight, error) {
	id, err := userID32(userID)
	if err != nil {
		return nil, errors.New("store: 興味度を更新できません")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, errors.New("store: 興味度を更新できません")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := r.q.WithTx(tx)
	if _, err = q.LockUserForRecommendation(ctx, id); err != nil {
		return nil, errors.New("store: 興味度を更新できません")
	}
	// 保存がクリックより先なら、先に反映してから減衰する。
	if err := applyDueSaveLearning(ctx, q, id); err != nil {
		return nil, err
	}
	rows, err := q.ListRecommendations(ctx, id)
	if err != nil {
		return nil, errors.New("store: 興味度を更新できません")
	}
	current := make([]interest.Weight, 0, len(rows))
	for _, row := range rows {
		current = append(current, interest.Weight{TagID: int64(row.TagID), Tag: row.TagName, Value: int64(row.MatchInt)})
	}
	updated, err := interest.UpdateOnClick(current, tags)
	if err != nil {
		return nil, err
	}
	// クリックAPIは、クライアントが送ったタグ名から tag 表の行を作らない（#239）。
	// まだ行の無いタグは、tag 表にすでにある名前だけを結びつけ、知らない名前は捨てる。
	known := updated[:0]
	for _, weight := range updated {
		if weight.TagID == 0 {
			row, e := q.FindTagByNormalizedName(ctx, weight.Tag)
			if errors.Is(e, pgx.ErrNoRows) {
				continue
			}
			if e != nil {
				return nil, errors.New("store: タグを確定できません")
			}
			weight.TagID = int64(row.TagID)
			weight.Tag = row.TagName
		}
		if weight.Value > math.MaxInt32 || weight.Value < math.MinInt32 {
			return nil, errors.New("store: 興味度を更新できません")
		}
		known = append(known, weight)
	}
	updated = known
	for _, weight := range updated {
		if err := q.UpsertRecommendation(ctx, dbgen.UpsertRecommendationParams{UserID: id, TagID: int32(weight.TagID), MatchInt: int32(weight.Value)}); err != nil {
			return nil, errors.New("store: 興味度を更新できません")
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, errors.New("store: 興味度を更新できません")
	}
	return updated, nil
}
