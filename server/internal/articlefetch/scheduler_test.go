package articlefetch

import (
	"context"
	"sync"
	"testing"
	"time"
)

type fakeRun struct {
	RunRow
	slot     time.Time
	trigger  string
	started  time.Time
	result   Result
	finishOK bool // 結果を記録した時の ctx が生きていたか
}

type fakeRuns struct {
	mu   sync.Mutex
	rows []*fakeRun
	next int64
}

func (r *fakeRuns) StartRun(_ context.Context, slot time.Time, trigger string, attempt int, startedAt time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.next++
	r.rows = append(r.rows, &fakeRun{RunRow: RunRow{ID: r.next, Attempt: attempt, Status: StatusRunning}, slot: slot, trigger: trigger, started: startedAt})
	return r.next, nil
}

func (r *fakeRuns) FinishRun(ctx context.Context, id int64, finishedAt time.Time, res Result) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, row := range r.rows {
		if row.ID == id {
			row.Status, row.FinishedAt, row.result, row.finishOK = res.Status, finishedAt, res, ctx.Err() == nil
		}
	}
	return nil
}

func (r *fakeRuns) RunsForSlot(_ context.Context, slot time.Time) ([]RunRow, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []RunRow
	for i := len(r.rows) - 1; i >= 0; i-- { // 新しい順
		if r.rows[i].slot.Equal(slot) {
			out = append(out, r.rows[i].RunRow)
		}
	}
	return out, nil
}

func (r *fakeRuns) AbandonStaleRuns(_ context.Context, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, row := range r.rows {
		if row.Status == StatusRunning {
			row.Status, row.FinishedAt = StatusFailed, at
		}
	}
	return nil
}

func (r *fakeRuns) add(slot time.Time, attempt int, status string, started, finished time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.next++
	row := &fakeRun{RunRow: RunRow{ID: r.next, Attempt: attempt, Status: status, FinishedAt: finished}, slot: slot, started: started}
	r.rows = append(r.rows, row)
}

func (r *fakeRuns) snapshot() []fakeRun {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]fakeRun, 0, len(r.rows))
	for _, row := range r.rows {
		out = append(out, *row)
	}
	return out
}

type fakeLocker struct {
	mu                    sync.Mutex
	ok                    bool
	tries, locks, unlocks int
}

func (l *fakeLocker) TryLock(context.Context) (func(), bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.tries++
	if !l.ok {
		return nil, false, nil
	}
	l.locks++
	return func() { l.mu.Lock(); l.unlocks++; l.mu.Unlock() }, true, nil
}

func (l *fakeLocker) set(ok bool) { l.mu.Lock(); l.ok = ok; l.mu.Unlock() }

func (l *fakeLocker) counts() (tries, locks, unlocks int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.tries, l.locks, l.unlocks
}

type fakeExec struct {
	mu          sync.Mutex
	results     []Result // 呼ばれた順に使う。尽きたら succeeded
	calls       int
	hadDeadline []bool
	during      func(ctx context.Context)
}

func (e *fakeExec) Execute(ctx context.Context) Result {
	e.mu.Lock()
	e.calls++
	var res Result
	if len(e.results) > 0 {
		res, e.results = e.results[0], e.results[1:]
	} else {
		res = Result{Status: StatusSucceeded}
	}
	_, ok := ctx.Deadline()
	e.hadDeadline = append(e.hadDeadline, ok)
	during := e.during
	e.mu.Unlock()
	if during != nil {
		during(ctx)
	}
	return res
}

func (e *fakeExec) count() int { e.mu.Lock(); defer e.mu.Unlock(); return e.calls }

type schedFixture struct {
	runs   *fakeRuns
	lock   *fakeLocker
	exec   *fakeExec
	clock  *fakeClock
	s      *Scheduler
	cancel context.CancelFunc
	done   chan struct{}
}

func newSchedFixture(now time.Time, results ...Result) *schedFixture {
	f := &schedFixture{
		runs:  &fakeRuns{},
		lock:  &fakeLocker{ok: true},
		exec:  &fakeExec{results: results},
		clock: newFakeClock(now),
		done:  make(chan struct{}),
	}
	f.s = &Scheduler{Runs: f.runs, Locker: f.lock, Job: f.exec, Clock: f.clock, Loc: Location(), RunTimeout: 20 * time.Minute}
	return f
}

func (f *schedFixture) start(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	f.cancel = cancel
	go func() { defer close(f.done); f.s.Run(ctx) }()
	t.Cleanup(func() { f.stop(t) })
}

func (f *schedFixture) stop(t *testing.T) {
	t.Helper()
	f.cancel()
	select {
	case <-f.done:
	case <-time.After(3 * time.Second):
		t.Fatal("取り消しても Run が戻らない")
	}
}

// settle は、スケジューラが次の待ちに入る（または終わる）まで待つ。
func (f *schedFixture) settle(t *testing.T) {
	t.Helper()
	waitFor(t, "次の待ち", func() bool { _, ok := f.clock.nextWaiter(); return ok })
}

func failed() Result { return Result{Status: StatusFailed, Message: "x"} }

func TestSchedulerCatchesUpLatestMissedSlotOnStart(t *testing.T) {
	f := newSchedFixture(jstTime(2026, 10, 22, 7, 0))
	f.start(t)
	waitFor(t, "追いつきの実行", func() bool { return f.exec.count() == 1 })
	f.settle(t)
	rows := f.runs.snapshot()
	if len(rows) != 1 || !rows[0].slot.Equal(jstTime(2026, 10, 22, 6, 0)) || rows[0].trigger != TriggerCatchup || rows[0].Attempt != 1 || rows[0].Status != StatusSucceeded {
		t.Fatalf("rows = %+v", rows)
	}
	// 次の定時に実行する(scheduled)
	f.clock.advanceTo(t, jstTime(2026, 10, 22, 11, 30))
	waitFor(t, "定時の実行", func() bool { return f.exec.count() == 2 })
	f.settle(t)
	rows = f.runs.snapshot()
	if !rows[1].slot.Equal(jstTime(2026, 10, 22, 11, 30)) || rows[1].trigger != TriggerScheduled {
		t.Fatalf("rows[1] = %+v", rows[1])
	}
}

func TestSchedulerCatchesUpOnlyTheLatestSlotEvenAfterManyMissed(t *testing.T) {
	// 前日の夜から止まっていて、翌日の昼に起動: 取り逃した枠は複数あるが、実行は直近の1回だけ。
	f := newSchedFixture(jstTime(2026, 10, 22, 13, 0))
	f.start(t)
	waitFor(t, "追いつき", func() bool { return f.exec.count() == 1 })
	f.settle(t)
	if f.exec.count() != 1 {
		t.Fatalf("calls = %d", f.exec.count())
	}
	if rows := f.runs.snapshot(); !rows[0].slot.Equal(jstTime(2026, 10, 22, 11, 30)) {
		t.Fatalf("slot = %v", rows[0].slot)
	}
}

func TestSchedulerDoesNotRunWhenSlotAlreadySucceededOrPartial(t *testing.T) {
	for _, status := range []string{StatusSucceeded, StatusPartial} {
		t.Run(status, func(t *testing.T) {
			f := newSchedFixture(jstTime(2026, 10, 22, 7, 0))
			f.runs.add(jstTime(2026, 10, 22, 6, 0), 1, status, jstTime(2026, 10, 22, 6, 0), jstTime(2026, 10, 22, 6, 3))
			f.start(t)
			f.settle(t)
			if f.exec.count() != 0 || len(f.runs.snapshot()) != 1 {
				t.Fatalf("calls = %d, rows = %d", f.exec.count(), len(f.runs.snapshot()))
			}
		})
	}
}

func TestSchedulerRetriesAfter15AndThen30MinutesThenStops(t *testing.T) {
	t0 := jstTime(2026, 10, 22, 6, 5)
	f := newSchedFixture(t0, failed(), failed(), failed())
	f.start(t)
	waitFor(t, "1回目", func() bool { return f.exec.count() == 1 })
	f.settle(t)

	f.clock.advanceTo(t, t0.Add(14*time.Minute+59*time.Second))
	time.Sleep(20 * time.Millisecond)
	if f.exec.count() != 1 {
		t.Fatalf("15分たつ前に再実行した: calls = %d", f.exec.count())
	}
	f.clock.advanceTo(t, t0.Add(15*time.Minute))
	waitFor(t, "2回目", func() bool { return f.exec.count() == 2 })
	f.settle(t)

	f.clock.advanceTo(t, t0.Add(15*time.Minute+29*time.Minute))
	time.Sleep(20 * time.Millisecond)
	if f.exec.count() != 2 {
		t.Fatalf("2回目の失敗から30分たつ前に再実行した: calls = %d", f.exec.count())
	}
	f.clock.advanceTo(t, t0.Add(45*time.Minute))
	waitFor(t, "3回目", func() bool { return f.exec.count() == 3 })
	f.settle(t)

	// 3回失敗したら、次の定時まで何もしない
	f.clock.advanceTo(t, jstTime(2026, 10, 22, 11, 29))
	time.Sleep(20 * time.Millisecond)
	if f.exec.count() != 3 {
		t.Fatalf("calls = %d", f.exec.count())
	}
	rows := f.runs.snapshot()
	wantTriggers := []string{TriggerCatchup, TriggerRetry, TriggerRetry}
	for i, row := range rows {
		if row.Attempt != i+1 || row.trigger != wantTriggers[i] || row.Status != StatusFailed || !row.slot.Equal(jstTime(2026, 10, 22, 6, 0)) {
			t.Fatalf("rows[%d] = %+v", i, row)
		}
	}
	if !rows[1].started.Equal(t0.Add(15*time.Minute)) || !rows[2].started.Equal(t0.Add(45*time.Minute)) {
		t.Fatalf("started = %v, %v", rows[1].started, rows[2].started)
	}
	// 次の定時には、また1回目から
	f.clock.advanceTo(t, jstTime(2026, 10, 22, 11, 30))
	waitFor(t, "次の定時", func() bool { return f.exec.count() == 4 })
	if rows := f.runs.snapshot(); rows[3].Attempt != 1 || rows[3].trigger != TriggerScheduled {
		t.Fatalf("rows[3] = %+v", rows[3])
	}
}

func TestSchedulerStopsRetryingAfterSuccess(t *testing.T) {
	t0 := jstTime(2026, 10, 22, 6, 5)
	f := newSchedFixture(t0, failed(), Result{Status: StatusPartial})
	f.start(t)
	waitFor(t, "1回目", func() bool { return f.exec.count() == 1 })
	f.settle(t)
	f.clock.advanceTo(t, t0.Add(15*time.Minute))
	waitFor(t, "2回目", func() bool { return f.exec.count() == 2 })
	f.settle(t)
	f.clock.advanceTo(t, t0.Add(2*time.Hour))
	time.Sleep(20 * time.Millisecond)
	if f.exec.count() != 2 {
		t.Fatalf("成功の後も再実行した: calls = %d", f.exec.count())
	}
}

func TestSchedulerSkipsRetryThatWouldPassNextSlot(t *testing.T) {
	// 16:20 に始まった11:30の追いつきが失敗。15分後(16:35)は次の定時(16:30)を過ぎるので、再実行しない。
	f := newSchedFixture(jstTime(2026, 10, 22, 16, 20), failed())
	f.start(t)
	waitFor(t, "追いつき", func() bool { return f.exec.count() == 1 })
	f.settle(t)
	f.clock.advanceTo(t, jstTime(2026, 10, 22, 16, 29))
	time.Sleep(20 * time.Millisecond)
	if f.exec.count() != 1 {
		t.Fatalf("calls = %d", f.exec.count())
	}
	f.clock.advanceTo(t, jstTime(2026, 10, 22, 16, 30))
	waitFor(t, "16:30の定時", func() bool { return f.exec.count() == 2 })
	if rows := f.runs.snapshot(); !rows[1].slot.Equal(jstTime(2026, 10, 22, 16, 30)) || rows[1].Attempt != 1 {
		t.Fatalf("rows[1] = %+v", rows[1])
	}
}

func TestSchedulerStartupWaitsForRetryDelayOfEarlierFailures(t *testing.T) {
	slot := jstTime(2026, 10, 22, 6, 0)
	f := newSchedFixture(jstTime(2026, 10, 22, 6, 20))
	f.runs.add(slot, 1, StatusFailed, slot, slot.Add(2*time.Minute))
	f.runs.add(slot, 2, StatusFailed, slot.Add(17*time.Minute), jstTime(2026, 10, 22, 6, 19))
	f.start(t)
	f.settle(t)
	if f.exec.count() != 0 {
		t.Fatalf("30分たつ前に実行した: calls = %d", f.exec.count())
	}
	f.clock.advanceTo(t, jstTime(2026, 10, 22, 6, 48))
	time.Sleep(20 * time.Millisecond)
	if f.exec.count() != 0 {
		t.Fatalf("calls = %d", f.exec.count())
	}
	f.clock.advanceTo(t, jstTime(2026, 10, 22, 6, 49))
	waitFor(t, "3回目", func() bool { return f.exec.count() == 1 })
	if rows := f.runs.snapshot(); rows[2].Attempt != 3 || rows[2].trigger != TriggerRetry {
		t.Fatalf("rows[2] = %+v", rows[2])
	}
}

func TestSchedulerDoesNotRetryAfterThreeFailures(t *testing.T) {
	slot := jstTime(2026, 10, 22, 6, 0)
	f := newSchedFixture(jstTime(2026, 10, 22, 8, 0))
	for i := 1; i <= 3; i++ {
		f.runs.add(slot, i, StatusFailed, slot, slot.Add(time.Minute))
	}
	f.start(t)
	f.settle(t)
	if f.exec.count() != 0 {
		t.Fatalf("calls = %d", f.exec.count())
	}
}

func TestSchedulerAbandonsStaleRunningRowThenRetries(t *testing.T) {
	slot := jstTime(2026, 10, 22, 6, 0)
	now := jstTime(2026, 10, 22, 6, 5)
	f := newSchedFixture(now)
	f.runs.add(slot, 1, StatusRunning, slot, time.Time{}) // 実行中に止まった行
	f.start(t)
	f.settle(t)
	rows := f.runs.snapshot()
	if rows[0].Status != StatusFailed || !rows[0].FinishedAt.Equal(now) {
		t.Fatalf("rows[0] = %+v", rows[0])
	}
	f.clock.advanceTo(t, now.Add(15*time.Minute))
	waitFor(t, "2回目", func() bool { return f.exec.count() == 1 })
	if rows := f.runs.snapshot(); rows[1].Attempt != 2 {
		t.Fatalf("rows[1] = %+v", rows[1])
	}
}

func TestSchedulerSkipsWithoutRecordingWhenLockIsHeldElsewhere(t *testing.T) {
	f := newSchedFixture(jstTime(2026, 10, 22, 7, 0))
	f.lock.set(false)
	f.start(t)
	f.settle(t)
	if tries, locks, _ := f.lock.counts(); tries < 1 || locks != 0 {
		t.Fatalf("tries = %d, locks = %d", tries, locks)
	}
	if f.exec.count() != 0 || len(f.runs.snapshot()) != 0 {
		t.Fatalf("calls = %d, rows = %d", f.exec.count(), len(f.runs.snapshot()))
	}
	// ロックが取れるようになれば、次の定時に実行する
	f.lock.set(true)
	f.clock.advanceTo(t, jstTime(2026, 10, 22, 11, 30))
	waitFor(t, "実行", func() bool { return f.exec.count() == 1 })
}

func TestSchedulerReleasesLockAfterEveryStep(t *testing.T) {
	f := newSchedFixture(jstTime(2026, 10, 22, 7, 0))
	f.start(t)
	waitFor(t, "実行", func() bool { return f.exec.count() == 1 })
	f.settle(t)
	if _, locks, unlocks := f.lock.counts(); locks != 1 || unlocks != 1 {
		t.Fatalf("locks = %d, unlocks = %d", locks, unlocks)
	}
}

func TestSchedulerAnotherInstanceAlreadyRanTheSlot(t *testing.T) {
	// 定時になって待ちから戻っても、ロックを取ってから履歴を見るので、他が実行済みなら重ねて実行しない。
	f := newSchedFixture(jstTime(2026, 10, 22, 11, 0))
	f.runs.add(jstTime(2026, 10, 22, 6, 0), 1, StatusSucceeded, jstTime(2026, 10, 22, 6, 0), jstTime(2026, 10, 22, 6, 1))
	f.start(t)
	f.settle(t)
	f.runs.add(jstTime(2026, 10, 22, 11, 30), 1, StatusSucceeded, jstTime(2026, 10, 22, 11, 30), jstTime(2026, 10, 22, 11, 31))
	f.clock.advanceTo(t, jstTime(2026, 10, 22, 11, 30))
	f.settle(t)
	time.Sleep(20 * time.Millisecond)
	if f.exec.count() != 0 {
		t.Fatalf("calls = %d", f.exec.count())
	}
}

func TestSchedulerPassesRunTimeoutAndRecordsResult(t *testing.T) {
	f := newSchedFixture(jstTime(2026, 10, 22, 7, 0), Result{Status: StatusPartial, Message: "m", QiitaRequests: 5, ZennRequests: 6, Upserted: 7, Deleted: 8})
	f.start(t)
	waitFor(t, "実行", func() bool { return f.exec.count() == 1 })
	f.settle(t)
	if !f.exec.hadDeadline[0] {
		t.Fatal("実行の上限が渡されていない")
	}
	got := f.runs.snapshot()[0].result
	if got.Status != StatusPartial || got.Message != "m" || got.QiitaRequests != 5 || got.ZennRequests != 6 || got.Upserted != 7 || got.Deleted != 8 {
		t.Fatalf("result = %+v", got)
	}
}

func TestSchedulerReturnsPromptlyWhenCanceledWhileWaiting(t *testing.T) {
	f := newSchedFixture(jstTime(2026, 10, 22, 7, 0))
	f.start(t)
	f.settle(t)
	f.stop(t) // 3秒以内に戻らなければ失敗
}

func TestSchedulerRecordsResultEvenWhenCanceledDuringRun(t *testing.T) {
	f := newSchedFixture(jstTime(2026, 10, 22, 7, 0), failed())
	started := make(chan struct{})
	f.exec.during = func(ctx context.Context) {
		close(started)
		<-ctx.Done()
	}
	f.start(t)
	<-started
	f.stop(t)
	rows := f.runs.snapshot()
	if len(rows) != 1 || rows[0].Status != StatusFailed || !rows[0].finishOK {
		t.Fatalf("rows = %+v", rows)
	}
	if f.exec.count() != 1 {
		t.Fatalf("止められた後に再実行した: calls = %d", f.exec.count())
	}
}

func TestSchedulerWakesUpAfterClockJump(t *testing.T) {
	// 待ちは10分ずつに区切るので、時計が飛んで定時を過ぎていても、次の区切りで気づく。
	f := newSchedFixture(jstTime(2026, 10, 22, 7, 0))
	f.start(t)
	waitFor(t, "追いつき", func() bool { return f.exec.count() == 1 })
	f.settle(t)
	if w := f.clock.Waits(); w[len(w)-1] != 10*time.Minute {
		t.Fatalf("待ちが10分に区切られていない: %v", w)
	}
	f.clock.Set(jstTime(2026, 10, 22, 17, 0)) // スリープから復帰して、11:30と16:30を過ぎた
	f.clock.fire(f.clock.Now())
	waitFor(t, "直近の1回", func() bool { return f.exec.count() == 2 })
	f.settle(t)
	if f.exec.count() != 2 {
		t.Fatalf("calls = %d", f.exec.count())
	}
	if rows := f.runs.snapshot(); !rows[1].slot.Equal(jstTime(2026, 10, 22, 16, 30)) {
		t.Fatalf("slot = %v", rows[1].slot)
	}
}

func TestDecide(t *testing.T) {
	now := jstTime(2026, 10, 22, 7, 0)
	at := func(h, m int) time.Time { return jstTime(2026, 10, 22, h, m) }
	for _, tc := range []struct {
		name string
		rows []RunRow
		want decision
	}{
		{"履歴なし", nil, decision{attempt: 1}},
		{"成功", []RunRow{{Attempt: 1, Status: StatusSucceeded}}, decision{skip: true}},
		{"一部成功", []RunRow{{Attempt: 2, Status: StatusPartial}, {Attempt: 1, Status: StatusFailed}}, decision{skip: true}},
		{"1回失敗", []RunRow{{Attempt: 1, Status: StatusFailed, FinishedAt: at(6, 3)}}, decision{attempt: 2, notBefore: at(6, 18)}},
		{"2回失敗", []RunRow{{Attempt: 2, Status: StatusFailed, FinishedAt: at(6, 20)}, {Attempt: 1, Status: StatusFailed, FinishedAt: at(6, 3)}}, decision{attempt: 3, notBefore: at(6, 50)}},
		{"3回失敗", []RunRow{{Attempt: 3, Status: StatusFailed, FinishedAt: at(6, 50)}}, decision{skip: true}},
		{"終了時刻なしの失敗は今を基準にする", []RunRow{{Attempt: 1, Status: StatusFailed}}, decision{attempt: 2, notBefore: now.Add(15 * time.Minute)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := decide(tc.rows, now)
			if got.skip != tc.want.skip || got.attempt != tc.want.attempt || !got.notBefore.Equal(tc.want.notBefore) {
				t.Fatalf("decide = %+v, want %+v", got, tc.want)
			}
		})
	}
}
