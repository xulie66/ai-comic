package jobs

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

func testManager(t *testing.T, root string, worker func(context.Context, Task) error) *Manager {
	t.Helper()
	m, e := New(root, worker, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(m.Close)
	return m
}
func startFixture(t *testing.T, m *Manager, uid, pid string, pages []int, c int) Status {
	t.Helper()
	s, e := m.Start(context.Background(), uid, pid, pages, json.RawMessage(`{"fixture":true}`), c)
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func receiveTask(t *testing.T, ch <-chan Task) Task {
	t.Helper()
	select {
	case task := <-ch:
		return task
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not start")
		return Task{}
	}
}
func waitStatus(t *testing.T, m *Manager, uid, pid string, predicate func(Status) bool) Status {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		s := m.Status(uid, pid)
		if predicate(s) {
			return s
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatalf("status did not converge: %+v", s)
			return s
		}
	}
}
func noTask(t *testing.T, ch <-chan Task) {
	t.Helper()
	select {
	case task := <-ch:
		t.Fatalf("unexpected dispatched task: %+v", task)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestProjectAndUserIsolation(t *testing.T) {
	started := make(chan Task, 16)
	release := make(chan struct{})
	m := testManager(t, t.TempDir(), func(ctx context.Context, task Task) error {
		started <- task
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	for _, pair := range [][2]string{{"alice", "book-a"}, {"alice", "book-b"}, {"bob", "book-a"}} {
		startFixture(t, m, pair[0], pair[1], []int{1}, 1)
	}
	seen := map[string]bool{}
	for i := 0; i < 3; i++ {
		task := receiveTask(t, started)
		seen[key(task.UserID, task.ProjectID)] = true
	}
	if len(seen) != 3 {
		t.Fatalf("projects collapsed: %v", seen)
	}
	if _, e := m.Pause("alice", "book-a"); e != nil {
		t.Fatal(e)
	}
	if m.Status("alice", "book-b").State != "running" || m.Status("bob", "book-a").State != "running" {
		t.Fatal("pause affected another tenant or project")
	}
	if m.Status("bob", "book-b").State != "idle" {
		t.Fatal("unknown project exposed another run")
	}
	close(release)
	waitStatus(t, m, "alice", "book-b", func(s Status) bool { return s.State == "done" && s.Done == 1 })
	waitStatus(t, m, "bob", "book-a", func(s Status) bool { return s.State == "done" && s.Done == 1 })
	waitStatus(t, m, "alice", "book-a", func(s Status) bool { return s.State == "paused" && s.Done == 1 && s.Active == 0 })
}

func TestGlobalAndPerProjectConcurrencyLimits(t *testing.T) {
	started := make(chan Task, 64)
	release := make(chan struct{})
	var mu sync.Mutex
	global, maxGlobal := 0, 0
	active := map[string]int{}
	maximum := map[string]int{}
	m := testManager(t, t.TempDir(), func(ctx context.Context, task Task) error {
		mu.Lock()
		global++
		if global > maxGlobal {
			maxGlobal = global
		}
		active[task.ProjectID]++
		if active[task.ProjectID] > maximum[task.ProjectID] {
			maximum[task.ProjectID] = active[task.ProjectID]
		}
		mu.Unlock()
		started <- task
		select {
		case <-release:
		case <-ctx.Done():
		}
		mu.Lock()
		global--
		active[task.ProjectID]--
		mu.Unlock()
		return ctx.Err()
	})
	for _, pid := range []string{"a", "b", "c"} {
		s := startFixture(t, m, "u", pid, []int{1, 2, 3, 4, 5, 6, 7, 8}, 99)
		if s.Concurrency != 4 {
			t.Fatalf("concurrency not clamped: %+v", s)
		}
	}
	for i := 0; i < 8; i++ {
		receiveTask(t, started)
	}
	noTask(t, started)
	mu.Lock()
	if maxGlobal != 8 {
		t.Errorf("global active max=%d want 8", maxGlobal)
	}
	for pid, n := range maximum {
		if n > 4 {
			t.Errorf("project %s active max=%d", pid, n)
		}
	}
	mu.Unlock()
	close(release)
	for _, pid := range []string{"a", "b", "c"} {
		waitStatus(t, m, "u", pid, func(s Status) bool { return s.State == "done" && s.Done == 8 })
	}
	mu.Lock()
	defer mu.Unlock()
	if maxGlobal > 8 {
		t.Errorf("global concurrency exceeded: %d", maxGlobal)
	}
	for pid, n := range maximum {
		if n > 4 {
			t.Errorf("project %s concurrency exceeded: %d", pid, n)
		}
	}
}

func TestPauseAllowsInflightCompletionWithoutDispatch(t *testing.T) {
	started := make(chan Task, 8)
	release := make(chan struct{}, 8)
	m := testManager(t, t.TempDir(), func(ctx context.Context, task Task) error {
		started <- task
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	startFixture(t, m, "u", "p", []int{1, 2, 3}, 1)
	receiveTask(t, started)
	if _, e := m.Pause("u", "p"); e != nil {
		t.Fatal(e)
	}
	release <- struct{}{}
	waitStatus(t, m, "u", "p", func(s Status) bool { return s.State == "paused" && s.Done == 1 && s.Active == 0 && s.Remaining == 2 })
	noTask(t, started)
	if _, e := m.Resume("u", "p"); e != nil {
		t.Fatal(e)
	}
	receiveTask(t, started)
	release <- struct{}{}
	receiveTask(t, started)
	release <- struct{}{}
	waitStatus(t, m, "u", "p", func(s Status) bool { return s.State == "done" && s.Done == 3 })
}

func TestStoppedInflightCompletionDoesNotChangeNewRun(t *testing.T) {
	started := make(chan Task, 8)
	oldRelease := make(chan struct{})
	newRelease := make(chan struct{})
	m := testManager(t, t.TempDir(), func(ctx context.Context, task Task) error {
		started <- task
		gate := newRelease
		if task.Page == 1 {
			gate = oldRelease
		}
		select {
		case <-gate:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	startFixture(t, m, "u", "p", []int{1, 9}, 1)
	old := receiveTask(t, started)
	if _, e := m.Stop("u", "p"); e != nil {
		t.Fatal(e)
	}
	startFixture(t, m, "u", "p", []int{2}, 1)
	fresh := receiveTask(t, started)
	if fresh.ID == old.ID || fresh.Page != 2 {
		t.Fatalf("new run task invalid: %+v", fresh)
	}
	close(oldRelease)
	// Wait for old execute to finish, rather than relying on scheduling delay.
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		m.mu.Lock()
		n := m.active
		m.mu.Unlock()
		if n == 1 {
			break
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("old worker did not finish")
		}
	}
	s := m.Status("u", "p")
	if s.Total != 1 || s.Done != 0 || s.Active != 1 || len(s.Current) != 1 || s.Current[0] != 2 {
		t.Fatalf("old completion polluted replacement run: %+v", s)
	}
	noTask(t, started)
	close(newRelease)
	waitStatus(t, m, "u", "p", func(s Status) bool { return s.State == "done" && s.Done == 1 && s.Total == 1 })
}

func TestRestartRecoversInterruptedAndPausedRuns(t *testing.T) {
	root := t.TempDir()
	started := make(chan Task, 8)
	first := testManager(t, root, func(ctx context.Context, task Task) error { started <- task; <-ctx.Done(); return ctx.Err() })
	startFixture(t, first, "u", "running", []int{1, 2}, 1)
	receiveTask(t, started)
	startFixture(t, first, "u", "paused", []int{3, 4}, 1)
	if _, e := first.Pause("u", "paused"); e != nil {
		t.Fatal(e)
	}
	first.Close()
	recovered := make(chan Task, 16)
	second := testManager(t, root, func(ctx context.Context, task Task) error { recovered <- task; return nil })
	waitStatus(t, second, "u", "running", func(s Status) bool { return s.State == "done" && s.Done == 2 })
	s := second.Status("u", "paused")
	if s.State != "paused" || s.Active != 0 || s.Remaining != 2 {
		t.Fatalf("paused run not recovered: %+v", s)
	}
	for len(recovered) > 0 {
		task := <-recovered
		if task.ProjectID != "running" {
			t.Fatalf("paused task dispatched at restart: %+v", task)
		}
	}
	if _, e := second.Resume("u", "paused"); e != nil {
		t.Fatal(e)
	}
	waitStatus(t, second, "u", "paused", func(s Status) bool { return s.State == "done" && s.Done == 2 })
}

func TestSubscriptionsAreUserScopedAndCancellationIdempotent(t *testing.T) {
	m := testManager(t, t.TempDir(), func(context.Context, Task) error { return nil })
	alice, cancelAlice := m.Subscribe("alice")
	bob, cancelBob := m.Subscribe("bob")
	defer cancelBob()
	m.Publish("alice", Event{"stage": "fixture", "projectId": "private-project"})
	select {
	case e := <-alice:
		if e["projectId"] != "private-project" {
			t.Fatalf("wrong event %v", e)
		}
	case <-time.After(time.Second):
		t.Fatal("missing user event")
	}
	select {
	case e := <-bob:
		t.Fatalf("cross-user event: %v", e)
	default:
	}
	cancelAlice()
	cancelAlice()
	if _, ok := <-alice; ok {
		t.Fatal("canceled subscription remains open")
	}
	m.Publish("alice", Event{"stage": "after-cancel"})
}

func TestSubscriptionCancelAfterManagerClose(t *testing.T) {
	m := testManager(t, t.TempDir(), func(context.Context, Task) error { return nil })
	ch, cancel := m.Subscribe("alice")
	m.Close()
	if _, ok := <-ch; ok {
		t.Fatal("Close left subscriber open")
	}
	// HTTP SSE handlers defer cancellation; shutdown may close the manager first.
	cancel()
	cancel()
}
