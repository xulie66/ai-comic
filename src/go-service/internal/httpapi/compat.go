package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"comic.local/go-service/internal/store"
)

// compat handles implemented legacy operations. Call after authentication and
// resolving the active project, before consuming the request body.
func (s *Server) compat(w http.ResponseWriter, r *http.Request, u store.PublicUser, pid string) (bool, error) {
	switch r.URL.Path {
	case "/api/task/create", "/api/task/complete", "/api/image/copy", "/api/scenes/rebuild":
	default:
		return false, nil
	}
	if r.Method != "POST" {
		return true, store.ErrInvalid
	}
	var p map[string]any
	if r.URL.Path == "/api/image/copy" {
		var raw json.RawMessage
		err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&raw)
		if err != nil {
			return true, store.ErrInvalid
		}
		var name string
		if json.Unmarshal(raw, &name) == nil {
			p = map[string]any{"sourcePath": name}
		} else if json.Unmarshal(raw, &p) != nil {
			return true, store.ErrInvalid
		}
	} else {
		var err error
		p, err = s.body(w, r)
		if err != nil {
			return true, err
		}
	}
	var out map[string]any
	var err error
	switch r.URL.Path {
	case "/api/task/create", "/api/task/complete":
		out, err = s.compatTask(r.Context(), u.ID, pid, r.URL.Path, p)
	case "/api/image/copy":
		out, err = s.compatCopy(r.Context(), u.ID, pid, p)
	case "/api/scenes/rebuild":
		out, err = s.compatScenes(r.Context(), u.ID, pid, p)
	}
	if err == nil {
		out["success"] = true
		s.json(w, 200, out)
	}
	return true, err
}
func (s *Server) compatTask(ctx context.Context, uid, pid, action string, p map[string]any) (map[string]any, error) {
	root, err := s.Store.ProjectRoot(ctx, uid, pid)
	if err != nil {
		return nil, err
	}
	h, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer h.Close()
	id := str(p, "taskId")
	creating := strings.HasSuffix(action, "/create")
	if creating {
		id = store.NewID("task")
	}
	if !strings.HasPrefix(id, "task_") || len(id) > 100 || strings.ContainsAny(id, "/\\:.\x00") {
		return nil, store.ErrInvalid
	}
	if err = h.MkdirAll("logs/tasks", 0700); err != nil {
		return nil, err
	}
	name := filepath.Join("logs", "tasks", id+".json")
	task := map[string]any{}
	if creating {
		task["taskId"] = id
		task["state"] = "running"
		task["startedAt"] = time.Now().UnixMilli()
	} else {
		raw, e := h.ReadFile(name)
		if errors.Is(e, os.ErrNotExist) {
			return nil, store.ErrNotFound
		}
		if e != nil {
			return nil, e
		}
		if json.Unmarshal(raw, &task) != nil {
			return nil, store.ErrInvalid
		}
		if task["state"] == "completed" {
			return map[string]any{"taskId": id, "state": "completed"}, nil
		}
		task["state"] = "completed"
		task["finishedAt"] = time.Now().UnixMilli()
		// Persist only bounded operational counters, never arbitrary logs or settings.
		data := object(p["data"])
		for _, key := range []string{"totalPages", "totalDurationSeconds", "failedPages"} {
			if v, ok := data[key].(float64); ok && v >= 0 {
				task[key] = v
			}
		}
	}
	b, err := json.Marshal(task)
	if err != nil {
		return nil, err
	}
	tmp := filepath.Join("logs", "tasks", store.NewID(".tmp"))
	f, err := h.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, err
	}
	defer h.Remove(tmp)
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err != nil {
		return nil, err
	}
	if ce != nil {
		return nil, ce
	}
	if err = h.Rename(tmp, name); err != nil {
		return nil, err
	}
	return map[string]any{"taskId": id, "state": task["state"], "startedAt": task["startedAt"]}, nil
}
func (s *Server) compatCopy(ctx context.Context, uid, pid string, p map[string]any) (map[string]any, error) {
	name := first(str(p, "sourcePath"), first(str(p, "imagePath"), str(p, "path")))
	source := str(p, "sourceProjectId")
	if source == "" {
		source = pid
	}
	b, err := s.Store.ReadImage(ctx, uid, source, name)
	if err != nil {
		return nil, err
	}
	mime := http.DetectContentType(b)
	ext := ""
	switch mime {
	case "image/png":
		ext = ".png"
	case "image/jpeg":
		ext = ".jpg"
	case "image/webp":
		ext = ".webp"
	case "image/gif":
		ext = ".gif"
	default:
		return nil, store.ErrInvalid
	}
	saved, err := s.Store.WriteImage(ctx, uid, pid, "output/"+store.NewID("copy")+ext, b)
	if err != nil {
		return nil, err
	}
	return map[string]any{"path": saved, "imagePath": saved, "canceled": false}, nil
}
func (s *Server) compatScenes(ctx context.Context, uid, pid string, p map[string]any) (map[string]any, error) {
	var out map[string]any
	rebuild := func(state map[string]any) error {
		sb := object(state["storyboard"])
		if v, ok := p["storyboard"].(map[string]any); ok {
			sb = v
		}
		if len(array(sb["pages"])) == 0 {
			return store.ErrInvalid
		}
		existing := append(referenceCards(state["sceneLibrary"]), referenceCards(sb["sceneLibrary"])...)
		library := []any{}
		seen := map[string]bool{}
		total, renamed := 0, 0
		visit := func(card map[string]any) {
			for _, key := range []string{"scene", "scene_name"} {
				name := strings.TrimSpace(str(card, key))
				if name == "" {
					continue
				}
				total++
				canonical := name
				var chosen map[string]any
				for _, candidate := range existing {
					if referenceMatches(candidate, name) {
						chosen = candidate
						canonical = first(str(candidate, "canonical_name"), str(candidate, "name"))
						break
					}
				}
				if canonical == "" {
					canonical = name
				}
				if canonical != name {
					renamed++
				}
				card[key] = canonical
				if !seen[canonical] {
					seen[canonical] = true
					copy := map[string]any{}
					for k, v := range chosen {
						copy[k] = v
					}
					copy["name"] = canonical
					copy["canonical_name"] = canonical
					library = append(library, copy)
				}
			}
		}
		for _, v := range array(sb["pages"]) {
			page := object(v)
			visit(page)
			for _, pv := range array(page["panels"]) {
				visit(object(pv))
			}
		}
		state["storyboard"] = sb
		state["sceneLibrary"] = library
		sb["sceneLibrary"] = library
		out = map[string]any{"library": library, "canonical": map[string]any{"total": total, "renamed": renamed}, "dryRun": p["dryRun"] == true}
		return nil
	}
	if p["dryRun"] == true {
		state, e := s.readState(ctx, uid, pid)
		if e != nil {
			return nil, e
		}
		if e = rebuild(state); e != nil {
			return nil, e
		}
	} else if err := s.Store.UpdateState(ctx, uid, pid, rebuild); err != nil {
		return nil, err
	}
	return out, nil
}

// compatProjects enriches owned projects for the existing Vue project cards.
func (s *Server) compatProjects(ctx context.Context, uid string) ([]map[string]any, error) {
	list, err := s.Store.ListProjects(ctx, uid)
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for _, p := range list {
		row := map[string]any{"id": p.ID, "name": p.Name, "createdAt": p.CreatedAt, "updatedAt": p.UpdatedAt, "legacy": p.Legacy}
		state, e := s.readState(ctx, uid, p.ID)
		if e != nil {
			return nil, e
		}
		pages := array(object(state["storyboard"])["pages"])
		done, panels := 0, 0
		cover := ""
		for _, pv := range pages {
			page := object(pv)
			panels += len(array(page["panels"]))
			if file := pageImage(page); file != "" {
				if _, e = s.Store.ImagePath(ctx, uid, p.ID, file); e == nil {
					if _, e = s.Store.ReadImage(ctx, uid, p.ID, file); e == nil {
						done++
						if cover == "" {
							cover = file
						}
					}
				}
			}
		}
		chars := referenceCards(state["characterLibrary"])
		if len(chars) == 0 {
			chars = referenceCards(object(state["storyboard"])["characterLibrary"])
		}
		row["pageCount"] = len(pages)
		row["doneCount"] = done
		row["panelCount"] = panels
		row["characterCount"] = len(chars)
		row["cover"] = cover
		row["coverProjectId"] = p.ID
		row["title"] = first(str(state, "title"), p.Name)
		out = append(out, row)
	}
	return out, nil
}
