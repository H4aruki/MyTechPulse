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

	"github.com/H4aruki/MyTechPulse/server/internal/auth"
	"github.com/H4aruki/MyTechPulse/server/internal/userfeedback"
)

const unreachableFeedbackDSN = "postgres://u:secretpw@127.0.0.1:1/db?sslmode=disable&connect_timeout=1"

// 接続しないpoolで作ったFeedback。入力検査で返る経路だけを呼ぶ。
func newUnreachableFeedback(t *testing.T) *Feedback {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), unreachableFeedbackDSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	return NewFeedback(pool)
}

func TestFeedbackParseUUID(t *testing.T) {
	const good = "0123abcd-4567-89ab-cdef-0123456789ab"
	u, err := parseUUID(good)
	if err != nil || !u.Valid {
		t.Fatalf("parseUUID(%q) = (%v, %v)", good, u, err)
	}
	for _, s := range []string{"", "not-a-uuid", "0123abcd-4567-89ab-cdef-0123456789a"} {
		if _, err := parseUUID(s); !errors.Is(err, userfeedback.ErrInvalidInput) {
			t.Errorf("parseUUID(%q): err = %v, want ErrInvalidInput", s, err)
		}
	}
}

func TestFeedbackUUIDTextRoundTrip(t *testing.T) {
	const want = "0123abcd-4567-89ab-cdef-0123456789ab"
	u, err := parseUUID(strings.ToUpper(want))
	if err != nil {
		t.Fatal(err)
	}
	if got := uuidText(u); got != want {
		t.Errorf("uuidText = %q, want %q", got, want)
	}
	if got := uuidText(pgtype.UUID{}); got != "00000000-0000-0000-0000-000000000000" {
		t.Errorf("zero uuidText = %q", got)
	}
}

func TestFeedbackTs(t *testing.T) {
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	got := ts(now)
	if !got.Valid || !got.Time.Equal(now) {
		t.Errorf("ts = %+v", got)
	}
}

func TestFeedbackUserID(t *testing.T) {
	for _, id := range []int64{0, -1, math.MaxInt32 + 1, math.MaxInt64} {
		_, err := feedbackUserID(id)
		if !errors.Is(err, userfeedback.ErrNotFound) {
			t.Errorf("id=%d: err = %v, want userfeedback.ErrNotFound", id, err)
		}
		if errors.Is(err, auth.ErrNotFound) {
			t.Errorf("id=%d: err must not be auth.ErrNotFound", id)
		}
	}
	for _, id := range []int64{1, math.MaxInt32} {
		got, err := feedbackUserID(id)
		if err != nil || int64(got) != id {
			t.Errorf("id=%d: got (%d, %v)", id, got, err)
		}
	}
}

func TestFeedbackRejectsBadInputBeforeDB(t *testing.T) {
	r := newUnreachableFeedback(t)
	ctx := context.Background()
	now := time.Now()
	const okID = "0123abcd-4567-89ab-cdef-0123456789ab"

	t.Run("範囲外のuserID", func(t *testing.T) {
		calls := map[string]error{}
		_, calls["LastPrompt"] = r.LastPrompt(ctx, 0, "k")
		_, calls["Present"] = r.Present(ctx, 0, okID, now)
		_, calls["SubmitOverall"] = r.SubmitOverall(ctx, 0, okID, 1, 1, now)
		_, calls["CompleteFollowup"] = r.CompleteFollowup(ctx, 0, okID, nil, now)
		_, calls["Dismiss"] = r.Dismiss(ctx, 0, okID, now)
		for name, err := range calls {
			if !errors.Is(err, userfeedback.ErrNotFound) {
				t.Errorf("%s: err = %v, want ErrNotFound", name, err)
			}
		}
	})

	t.Run("不正なID", func(t *testing.T) {
		calls := map[string]error{}
		_, calls["Present"] = r.Present(ctx, 1, "bad", now)
		_, calls["SubmitOverall"] = r.SubmitOverall(ctx, 1, "bad", 1, 1, now)
		_, calls["CompleteFollowup"] = r.CompleteFollowup(ctx, 1, "bad", nil, now)
		_, calls["Dismiss"] = r.Dismiss(ctx, 1, "bad", now)
		for name, err := range calls {
			if !errors.Is(err, userfeedback.ErrInvalidInput) {
				t.Errorf("%s: err = %v, want ErrInvalidInput", name, err)
			}
		}
	})
}

func TestFeedbackHidesDBErrorDetails(t *testing.T) {
	r := newUnreachableFeedback(t)
	check := func(name string, err error) {
		t.Helper()
		if !errors.Is(err, errFeedback) {
			t.Errorf("%s: err = %v, want errFeedback", name, err)
			return
		}
		for _, leak := range []string{"127.0.0.1", "secretpw"} {
			if strings.Contains(err.Error(), leak) {
				t.Errorf("%s: error leaks %q: %v", name, leak, err)
			}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := r.ActiveForm(ctx)
	check("ActiveForm", err)

	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	_, err = r.LastPrompt(ctx2, 1, "k")
	check("LastPrompt", err)
}
