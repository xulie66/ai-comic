package httpapi

import (
	"comic.local/go-service/internal/auth"
	"comic.local/go-service/internal/jobs"
	"comic.local/go-service/internal/store"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

func (s *Server) InitJobs(root string) error {
	m, e := jobs.New(root, func(ctx context.Context, t jobs.Task) error {
		u, e := s.Store.UserByID(ctx, t.UserID)
		if e != nil {
			return e
		}
		var payload map[string]any
		if e = json.Unmarshal(t.Payload, &payload); e != nil {
			return e
		}
		state, e := s.readState(ctx, t.UserID, t.ProjectID)
		if e != nil {
			return e
		}
		found := false
		for _, pv := range array(object(state["storyboard"])["pages"]) {
			p := object(pv)
			key, err := pageIndex(p)
			if err == nil && key == t.Page {
				payload["page"] = p
				found = true
				break
			}
		}
		if !found {
			return store.ErrNotFound
		}
		payload["storyboard"] = state["storyboard"]
		payload["_jobTaskRef"] = t.ID
		_, e = s.generate(ctx, u.Public(), t.ProjectID, "page", payload)
		if e == nil && s.Jobs != nil {
			s.Jobs.Publish(t.UserID, jobs.Event{"stage": "image_ready", "pageNumber": t.Page, "projectId": t.ProjectID})
		}
		return e
	}, s.Logger)
	if e == nil {
		s.Jobs = m
	}
	return e
}
func (s *Server) events(w http.ResponseWriter, r *http.Request, uid string) error {
	if s.Jobs == nil {
		return store.ErrNotFound
	}
	f, ok := w.(http.Flusher)
	if !ok {
		return store.ErrInvalid
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	ch, cancel := s.Jobs.Subscribe(uid)
	defer cancel()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	fmt.Fprint(w, ": connected\n\n")
	f.Flush()
	for {
		select {
		case <-r.Context().Done():
			return nil
		case <-ticker.C:
			fmt.Fprint(w, ": ping\n\n")
			f.Flush()
		case event, ok := <-ch:
			if !ok {
				return nil
			}
			b, _ := json.Marshal(event)
			fmt.Fprintf(w, "data: %s\n\n", b)
			f.Flush()
		}
	}
}
func (s *Server) render(w http.ResponseWriter, r *http.Request, u store.PublicUser) error {
	if s.Jobs == nil {
		return store.ErrNotFound
	}
	p, e := s.active(r.Context(), u.ID)
	if e != nil {
		return e
	}
	status := s.Jobs.Status(u.ID, p.ID)
	switch r.URL.Path {
	case "/api/render/start":
		payload, e := s.body(w, r)
		if e != nil {
			return e
		}
		state, e := s.readState(r.Context(), u.ID, p.ID)
		if e != nil {
			return e
		}
		indices := []int{}
		mode := first(str(payload, "mode"), "missing")
		wanted := map[string]bool{}
		for _, v := range array(payload["indices"]) {
			wanted[fmt.Sprint(v)] = true
		}
		for _, v := range array(object(state["storyboard"])["pages"]) {
			page := object(v)
			n, err := pageIndex(page)
			if err != nil {
				return err
			}
			key := fmt.Sprint(n)
			if mode == "list" && !wanted[key] {
				continue
			}
			if mode == "missing" && pageImage(page) != "" {
				continue
			}
			if n > 0 {
				indices = append(indices, n)
			}
		}
		raw, _ := json.Marshal(payload)
		concurrency := 2
		if n, ok := payload["concurrency"].(float64); ok {
			concurrency = int(n)
		}
		status, e = s.Jobs.Start(r.Context(), u.ID, p.ID, indices, raw, concurrency)
		if e != nil {
			return e
		}
	case "/api/render/pause":
		status, e = s.Jobs.Pause(u.ID, p.ID)
	case "/api/render/resume":
		status, e = s.Jobs.Resume(u.ID, p.ID)
	case "/api/render/stop":
		status, e = s.Jobs.Stop(u.ID, p.ID)
	case "/api/render/status":
	default:
		return store.ErrNotFound
	}
	if e != nil {
		return e
	}
	s.json(w, 200, map[string]any{"success": true, "status": status})
	return nil
}

var _ = auth.UserFromContext
