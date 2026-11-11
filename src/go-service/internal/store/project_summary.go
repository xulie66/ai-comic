package store

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
)

// ProjectSummary returns UI-safe metadata and statistics. File paths are never
// exposed directly; covers use the authenticated tenant-scoped file endpoint.
func (s *Store) ProjectSummary(ctx context.Context, owner, id string) (map[string]any, error) {
	p, err := s.GetProject(ctx, owner, id)
	if err != nil {
		return nil, err
	}
	raw, err := s.LoadState(ctx, owner, id)
	if err != nil {
		if (p.Legacy || p.Root == ".") && (errors.Is(err, ErrForbidden) || errors.Is(err, ErrNotFound)) {
			active := false
			current, e := s.ActiveProject(ctx, owner)
			if e == nil {
				active = current.ID == id
			} else if !errors.Is(e, ErrNotFound) {
				return nil, e
			}
			return map[string]any{"id": p.ID, "ownerId": p.OwnerID, "name": p.Name, "createdAt": p.CreatedAt, "updatedAt": p.UpdatedAt, "legacy": p.Legacy, "needsMigration": true, "pageCount": 0, "doneCount": 0, "panelCount": 0, "characterCount": 0, "cover": "", "title": "", "active": active}, nil
		}
		return nil, err
	}
	var state map[string]any
	if err = json.Unmarshal(raw, &state); err != nil {
		return nil, err
	}
	pages := stateArray(stateMap(state["storyboard"])["pages"])
	done, panels := 0, 0
	cover := ""
	for _, v := range pages {
		page := stateMap(v)
		panels += len(stateArray(page["panels"]))
		path := ""
		for _, field := range []string{"page_image_url", "imagePath", "pageImageUrl"} {
			if value := text(page[field]); value != "" {
				path = value
				break
			}
		}
		if path != "" {
			done++
			if cover == "" {
				relative, e := s.imageRelative(ctx, owner, id, path)
				if e == nil {
					q := url.Values{"projectId": []string{id}, "p": []string{strings.ReplaceAll(relative, "\\", "/")}}
					cover = "/files?" + q.Encode()
				} else if !errors.Is(e, ErrForbidden) && !errors.Is(e, ErrInvalid) {
					return nil, e
				}
			}
		}
	}
	characters := stateArray(state["characterLibrary"])
	if characters == nil {
		characters = stateArray(stateMap(state["storyboard"])["characterLibrary"])
	}
	title := strings.Join(strings.Fields(text(state["originalText"])), " ")
	runes := []rune(title)
	if len(runes) > 40 {
		title = string(runes[:40])
	}
	active := false
	current, e := s.ActiveProject(ctx, owner)
	if e == nil {
		active = current.ID == id
	} else if !errors.Is(e, ErrNotFound) {
		return nil, e
	}
	return map[string]any{"id": p.ID, "ownerId": p.OwnerID, "name": p.Name, "createdAt": p.CreatedAt, "updatedAt": p.UpdatedAt, "legacy": p.Legacy, "needsMigration": false, "pageCount": len(pages), "doneCount": done, "panelCount": panels, "characterCount": len(characters), "cover": cover, "title": title, "active": active}, nil
}

// SummarizeProjects rechecks each project's owner using the authoritative store;
// callers cannot supply another user's metadata to bypass isolation.
func (s *Store) SummarizeProjects(ctx context.Context, owner string, projects []Project) ([]map[string]any, error) {
	out := make([]map[string]any, 0, len(projects))
	for _, p := range projects {
		summary, err := s.ProjectSummary(ctx, owner, p.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, summary)
	}
	return out, nil
}
