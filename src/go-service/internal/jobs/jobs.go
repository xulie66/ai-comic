package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Event map[string]any
type Task struct {
	ID, UserID, ProjectID string
	Page                  int
	Payload               json.RawMessage
	Attempts              int
	State                 string
}
type Status struct {
	State          string  `json:"state"`
	Total          int     `json:"total"`
	Done           int     `json:"done"`
	Failed         int     `json:"failed"`
	Remaining      int     `json:"remaining"`
	Active         int     `json:"active"`
	Concurrency    int     `json:"concurrency"`
	Current        []int   `json:"current"`
	Queued         []int   `json:"queued"`
	ElapsedSeconds float64 `json:"elapsedSeconds"`
	AvgSeconds     float64 `json:"avgSeconds"`
	LastError      string  `json:"lastError"`
	AutoPaused     bool    `json:"autoPaused"`
	StartedAt      int64   `json:"startedAt"`
	FinishedAt     int64   `json:"finishedAt"`
	ProjectID      string  `json:"projectId"`
	FinishedPages  int     `json:"finishedPages"`
}
type run struct {
	UserID, ProjectID, State string
	Concurrency              int
	StartedAt, FinishedAt    int64
	Tasks                    []Task
	LastError                string
}
type Manager struct {
	mu     sync.Mutex
	root   string
	runs   map[string]*run
	subs   map[string]map[chan Event]bool
	worker func(context.Context, Task) error
	logger *slog.Logger
	ctx    context.Context
	cancel context.CancelFunc
	wake   chan struct{}
	wg     sync.WaitGroup
	active int
	closed bool
}

func key(uid, pid string) string { return uid + "/" + pid }
func New(root string, worker func(context.Context, Task) error, logger *slog.Logger) (*Manager, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if worker == nil {
		return nil, errors.New("worker required")
	}
	if e := os.MkdirAll(root, 0700); e != nil {
		return nil, e
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{root: root, runs: map[string]*run{}, subs: map[string]map[chan Event]bool{}, worker: worker, logger: logger, ctx: ctx, cancel: cancel, wake: make(chan struct{}, 1)}
	b, e := os.ReadFile(filepath.Join(root, "runs.json"))
	if e == nil {
		if e = json.Unmarshal(b, &m.runs); e != nil {
			return nil, e
		}
		for _, r := range m.runs {
			for i := range r.Tasks {
				t := &r.Tasks[i]
				if t.State == "running" {
					if t.Attempts >= 3 {
						t.State = "failed"
					} else {
						t.State = "queued"
					}
				}
			}
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return nil, e
	}
	m.wg.Add(1)
	go m.loop()
	m.signal()
	return m, nil
}
func (m *Manager) signal() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}
func (m *Manager) persist() error {
	b, e := json.Marshal(m.runs)
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(m.root, ".queue-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	_, e = f.Write(b)
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	return os.Rename(f.Name(), filepath.Join(m.root, "runs.json"))
}
func (m *Manager) Start(ctx context.Context, uid, pid string, pages []int, payload json.RawMessage, concurrency int) (Status, error) {
	if e := ctx.Err(); e != nil {
		return Status{}, e
	}
	if uid == "" || pid == "" || !json.Valid(payload) {
		return Status{}, errors.New("invalid task")
	}
	if concurrency < 1 {
		concurrency = 1
	}
	if concurrency > 4 {
		concurrency = 4
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Status{}, errors.New("closed")
	}
	k := key(uid, pid)
	r := m.runs[k]
	if r == nil || r.State == "done" || r.State == "stopped" {
		r = &run{UserID: uid, ProjectID: pid, Concurrency: concurrency, State: "running", StartedAt: time.Now().UnixMilli()}
		m.runs[k] = r
	}
	existing := map[int]bool{}
	for _, t := range r.Tasks {
		existing[t.Page] = true
	}
	for _, page := range pages {
		if page < 1 || existing[page] {
			continue
		}
		existing[page] = true
		r.Tasks = append(r.Tasks, Task{ID: fmt.Sprintf("%s-%d-%d", pid, time.Now().UnixNano(), page), UserID: uid, ProjectID: pid, Page: page, Payload: append(json.RawMessage(nil), payload...), State: "queued"})
	}
	r.Concurrency = concurrency
	r.State = "running"
	if e := m.persist(); e != nil {
		return Status{}, e
	}
	m.signal()
	return status(r), nil
}
func status(r *run) Status {
	if r == nil {
		return Status{State: "idle", Concurrency: 2, Current: []int{}, Queued: []int{}}
	}
	s := Status{State: r.State, Total: len(r.Tasks), Concurrency: r.Concurrency, StartedAt: r.StartedAt, FinishedAt: r.FinishedAt, ProjectID: r.ProjectID, LastError: r.LastError, Current: []int{}, Queued: []int{}}
	for _, t := range r.Tasks {
		switch t.State {
		case "done":
			s.Done++
		case "failed":
			s.Failed++
		case "running":
			s.Active++
			s.Current = append(s.Current, t.Page)
		case "queued":
			s.Remaining++
			s.Queued = append(s.Queued, t.Page)
		}
	}
	s.Remaining += s.Active
	s.FinishedPages = s.Done
	end := time.Now().UnixMilli()
	if r.FinishedAt > 0 {
		end = r.FinishedAt
	}
	if r.StartedAt > 0 {
		s.ElapsedSeconds = float64(end-r.StartedAt) / 1000
	}
	if s.Done > 0 {
		s.AvgSeconds = s.ElapsedSeconds / float64(s.Done)
	}
	return s
}
func (m *Manager) Status(uid, pid string) Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return status(m.runs[key(uid, pid)])
}
func (m *Manager) change(uid, pid, state string) (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.runs[key(uid, pid)]
	if r == nil {
		return status(nil), nil
	}
	r.State = state
	if state == "stopped" {
		for i := range r.Tasks {
			if r.Tasks[i].State == "queued" {
				r.Tasks[i].State = "canceled"
			}
		}
		r.FinishedAt = time.Now().UnixMilli()
	}
	if e := m.persist(); e != nil {
		return Status{}, e
	}
	m.publishLocked(uid, Event{"stage": "render_status", "projectId": pid, "render": status(r)})
	m.signal()
	return status(r), nil
}
func (m *Manager) Pause(uid, pid string) (Status, error)  { return m.change(uid, pid, "paused") }
func (m *Manager) Resume(uid, pid string) (Status, error) { return m.change(uid, pid, "running") }
func (m *Manager) Stop(uid, pid string) (Status, error)   { return m.change(uid, pid, "stopped") }
func (m *Manager) loop() {
	defer m.wg.Done()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-m.wake:
		case <-ticker.C:
		}
		m.dispatch()
	}
}
func (m *Manager) dispatch() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	for _, r := range m.runs {
		if r.State != "running" {
			continue
		}
		active := 0
		queued := 0
		for _, t := range r.Tasks {
			if t.State == "running" {
				active++
			}
			if t.State == "queued" {
				queued++
			}
		}
		if active == 0 && queued == 0 {
			r.State = "done"
			r.FinishedAt = time.Now().UnixMilli()
			_ = m.persist()
			m.publishLocked(r.UserID, Event{"stage": "render_done", "projectId": r.ProjectID, "render": status(r)})
			continue
		}
		for i := range r.Tasks {
			if m.active >= 8 || active >= r.Concurrency {
				break
			}
			t := &r.Tasks[i]
			if t.State != "queued" {
				continue
			}
			t.State = "running"
			t.Attempts++
			if e := m.persist(); e != nil {
				t.State = "queued"
				m.logger.Error("queue persistence failed", "error", e)
				break
			}
			active++
			m.active++
			copyTask := *t
			m.wg.Add(1)
			go m.execute(copyTask)
		}
		m.publishLocked(r.UserID, Event{"stage": "render_status", "projectId": r.ProjectID, "render": status(r)})
	}
}
func (m *Manager) execute(t Task) {
	defer m.wg.Done()
	e := m.worker(m.ctx, t)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.active--
	r := m.runs[key(t.UserID, t.ProjectID)]
	if r != nil {
		for i := range r.Tasks {
			if r.Tasks[i].ID == t.ID {
				if m.ctx.Err() != nil {
					r.Tasks[i].State = "queued"
				} else if e != nil {
					r.Tasks[i].State = "failed"
					r.LastError = "任务失败，详细信息已记录在服务端"
					m.logger.Error("generation task failed", "task", t.ID, "error", e)
				} else {
					r.Tasks[i].State = "done"
				}
				break
			}
		}
		if err := m.persist(); err != nil {
			m.logger.Error("queue completion persistence failed", "error", err)
		}
		m.publishLocked(t.UserID, Event{"stage": "render_status", "projectId": t.ProjectID, "render": status(r)})
	}
	m.signal()
}
func (m *Manager) Subscribe(uid string) (<-chan Event, func()) {
	m.mu.Lock()
	ch := make(chan Event, 32)
	if m.subs[uid] == nil {
		m.subs[uid] = map[chan Event]bool{}
	}
	m.subs[uid][ch] = true
	m.mu.Unlock()
	once := sync.Once{}
	return ch, func() {
		once.Do(func() {
			m.mu.Lock()
			defer m.mu.Unlock()
			if m.subs[uid][ch] {
				delete(m.subs[uid], ch)
				close(ch)
			}
		})
	}
}
func (m *Manager) publishLocked(uid string, e Event) {
	if uid == "" {
		return
	}
	for ch := range m.subs[uid] {
		select {
		case ch <- e:
		default:
		}
	}
}
func (m *Manager) Publish(uid string, e Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.publishLocked(uid, e)
}
func (m *Manager) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	m.cancel()
	m.mu.Unlock()
	m.wg.Wait()
	m.mu.Lock()
	defer m.mu.Unlock()
	_ = m.persist()
	for _, channels := range m.subs {
		for ch := range channels {
			close(ch)
		}
	}
	m.subs = map[string]map[chan Event]bool{}
}
