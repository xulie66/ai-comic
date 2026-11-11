package httpapi

import (
	"comic.local/go-service/internal/generation"
	"comic.local/go-service/internal/store"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
)

func (s *Server) assetPath(ctx context.Context, uid, pid string) (string, error) {
	root, e := s.Store.ProjectRoot(ctx, uid, pid)
	return filepath.Join(root, "projects", "asset-library.json"), e
}
func (s *Server) assets(w http.ResponseWriter, r *http.Request, u store.PublicUser, pid string) error {
	lib := map[string]any{"characters": []any{}, "scenes": []any{}, "props": []any{}}
	b, e := s.Store.LoadProjectJSON(r.Context(), u.ID, pid, "projects/asset-library.json")
	if e == nil {
		if json.Unmarshal(b, &lib) != nil {
			return store.ErrInvalid
		}
	} else if !errors.Is(e, store.ErrNotFound) {
		return e
	}
	if r.URL.Path == "/api/extract/saved" {
		s.json(w, 200, map[string]any{"success": true, "library": lib})
		return nil
	}
	p, e := s.body(w, r)
	if e != nil {
		return e
	}
	kind := str(p, "kind")
	if kind != "characters" && kind != "scenes" && kind != "props" {
		return store.ErrInvalid
	}
	items := array(p["items"])
	if str(p, "mode") != "replace" {
		merged := array(lib[kind])
		for _, iv := range items {
			item := object(iv)
			found := false
			for i, v := range merged {
				if str(object(v), "name") == str(item, "name") {
					merged[i] = item
					found = true
					break
				}
			}
			if !found {
				merged = append(merged, item)
			}
		}
		items = merged
	}
	lib[kind] = items
	raw, _ := json.Marshal(lib)
	if e = s.Store.SaveProjectJSON(r.Context(), u.ID, pid, "projects/asset-library.json", raw); e != nil {
		return e
	}
	if kind == "characters" || kind == "scenes" {
		e = s.Store.UpdateState(r.Context(), u.ID, pid, func(state map[string]any) error {
			field := "characterLibrary"
			if kind == "scenes" {
				field = "sceneLibrary"
			}
			state[field] = items
			return nil
		})
		if e != nil {
			return e
		}
	}
	s.json(w, 200, map[string]any{"success": true, "library": lib, "items": items})
	return nil
}
func (s *Server) tests(w http.ResponseWriter, r *http.Request, u store.PublicUser, pid string) error {
	if !u.IsAdmin {
		return store.ErrForbidden
	}
	capability := "text"
	if strings.HasSuffix(r.URL.Path, "image") {
		capability = "image"
	}
	c, _, e := s.generationConfig(r.Context(), u, pid, capability)
	if e != nil {
		return e
	}
	if capability == "text" {
		_, e = s.Client.Text(r.Context(), c, "请返回JSON：{\"ok\":true}")
	} else {
		_, _, e = s.Client.Image(r.Context(), c, "一只纯白杯子，白色背景，不含任何文字。", []generation.Reference{}, "1:1")
	}
	if e != nil {
		return e
	}
	s.json(w, 200, map[string]any{"success": true, "message": "连接测试成功"})
	return nil
}
