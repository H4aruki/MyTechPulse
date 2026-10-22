package articlefetch

import (
	"context"
	"log/slog"
	"time"
)

const (
	// maxAttempts は1つの定時に対する実行の最大回数（最初の1回＋全体の再実行2回）。
	maxAttempts = 3
	// maxSleep は待ちを区切る長さ。時計が飛んでも（スリープからの復帰など）、毎回 now から計算し直す。
	maxSleep = 10 * time.Minute
	// finishTimeout は、止められた後でも実行結果を記録するための待ち時間の上限。
	finishTimeout = 10 * time.Second
)

// retryDelay は、attempt 回目の失敗の後、次の実行まで待つ時間（1回目の失敗の15分後、2回目の失敗の30分後）。
func retryDelay(attempt int) time.Duration {
	if attempt <= 1 {
		return 15 * time.Minute
	}
	return 30 * time.Minute
}

// Scheduler は、定時になったら取得を行う。起動時には、直近の取り逃した定時を補う。
type Scheduler struct {
	Runs   RunStore
	Locker Locker
	Job    Executor
	Clock  Clock
	Logger *slog.Logger
	// Loc は定時を数えるタイムゾーン。nilなら日本時間。
	Loc *time.Location
	// RunTimeout は1回の実行の上限。0なら上限なし。
	RunTimeout time.Duration
}

// Run は ctx が終わるまで動く。まず直近の定時の取り逃しを補い、以後は定時ごとに取得する。
func (s *Scheduler) Run(ctx context.Context) {
	loc := s.location()
	s.process(ctx, LatestSlot(s.Clock.Now(), loc), TriggerCatchup)
	for ctx.Err() == nil {
		next := NextSlot(s.Clock.Now(), loc)
		if !s.sleepUntil(ctx, next) {
			return
		}
		// 時計が大きく飛んで複数の定時を過ぎていても、取得は直近の1回だけ（毎回すべて取り直すため）。
		s.process(ctx, LatestSlot(s.Clock.Now(), loc), TriggerScheduled)
	}
}

func (s *Scheduler) location() *time.Location {
	if s.Loc != nil {
		return s.Loc
	}
	return Location()
}

func (s *Scheduler) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.New(slog.DiscardHandler)
}

// process は1つの定時について、取得と、失敗したときの全体の再実行を行う。
func (s *Scheduler) process(ctx context.Context, slot time.Time, first string) {
	loc := s.location()
	for ctx.Err() == nil {
		retryAt, again := s.step(ctx, slot, first)
		if !again {
			return
		}
		if !retryAt.Before(NextSlot(slot, loc)) {
			s.logger().Info("article fetch: retry skipped, next slot comes first", "slot", slot)
			return
		}
		if !s.sleepUntil(ctx, retryAt) {
			return
		}
		first = TriggerRetry
	}
}

// step はロックを取り、履歴から実行するかを決めて、実行する。
// again が true なら、retryAt 以降にもう一度 step を呼ぶ。
func (s *Scheduler) step(ctx context.Context, slot time.Time, first string) (retryAt time.Time, again bool) {
	log := s.logger()
	unlock, ok, err := s.Locker.TryLock(ctx)
	if err != nil {
		if ctx.Err() == nil {
			log.Error("article fetch: lock failed")
		}
		return time.Time{}, false
	}
	if !ok {
		log.Info("article fetch: another instance is running, skipped", "slot", slot)
		return time.Time{}, false
	}
	defer unlock()

	now := s.Clock.Now()
	// ロックを持てた = 他に動いている実行は無い。'running' のまま残った行は止められたもの。
	if err := s.Runs.AbandonStaleRuns(ctx, now); err != nil {
		log.Error("article fetch: abandon stale runs failed")
		return time.Time{}, false
	}
	rows, err := s.Runs.RunsForSlot(ctx, slot)
	if err != nil {
		log.Error("article fetch: read runs failed")
		return time.Time{}, false
	}
	d := decide(rows, now)
	if d.skip {
		return time.Time{}, false
	}
	if d.notBefore.After(now) {
		return d.notBefore, true
	}

	trigger := first
	if d.attempt > 1 {
		trigger = TriggerRetry
	}
	id, err := s.Runs.StartRun(ctx, slot, trigger, d.attempt, now)
	if err != nil {
		log.Error("article fetch: start run failed")
		return time.Time{}, false
	}
	runCtx, cancel := ctx, context.CancelFunc(func() {})
	if s.RunTimeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, s.RunTimeout)
	}
	log.Info("article fetch started", "run_id", id, "slot", slot, "trigger", trigger, "attempt", d.attempt)
	res := s.Job.Execute(runCtx)
	cancel()

	finished := s.Clock.Now()
	// 止められた後でも結果を記録する。
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), finishTimeout)
	defer finishCancel()
	if err := s.Runs.FinishRun(finishCtx, id, finished, res); err != nil {
		log.Error("article fetch: finish run failed", "run_id", id)
	}
	if res.Status == StatusFailed && d.attempt < maxAttempts && ctx.Err() == nil {
		return finished.Add(retryDelay(d.attempt)), true
	}
	return time.Time{}, false
}

// decision は、ある定時の履歴から決めた次の動き。
type decision struct {
	skip      bool      // 何もしない
	attempt   int       // 実行するときの回数(1〜)
	notBefore time.Time // これより前には実行しない（ゼロ値なら今すぐ）
}

// decide は、定時 slot の履歴 rows(新しい順)から、実行するかを決める。
//   - 履歴が無い: 1回目を今すぐ
//   - 成功・一部成功がある: 何もしない
//   - 失敗だけ: 3回に満たなければ、最後の失敗から待ち時間(15分/30分)の後に次の回
func decide(rows []RunRow, now time.Time) decision {
	if len(rows) == 0 {
		return decision{attempt: 1}
	}
	maxAttempt := 0
	for _, r := range rows {
		switch r.Status {
		case StatusSucceeded, StatusPartial, StatusRunning:
			return decision{skip: true}
		}
		if r.Attempt > maxAttempt {
			maxAttempt = r.Attempt
		}
	}
	if maxAttempt >= maxAttempts {
		return decision{skip: true}
	}
	last := rows[0].FinishedAt
	if last.IsZero() {
		last = now
	}
	return decision{attempt: maxAttempt + 1, notBefore: last.Add(retryDelay(maxAttempt))}
}

// sleepUntil は t まで待つ。長い待ちは maxSleep ずつに区切り、毎回 now から計算し直す。
// ctx が終わったら false。
func (s *Scheduler) sleepUntil(ctx context.Context, t time.Time) bool {
	for {
		d := t.Sub(s.Clock.Now())
		if d <= 0 {
			return ctx.Err() == nil
		}
		if d > maxSleep {
			d = maxSleep
		}
		select {
		case <-s.Clock.After(d):
		case <-ctx.Done():
			return false
		}
	}
}
