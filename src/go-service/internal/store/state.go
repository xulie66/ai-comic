package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// UpdateState serializes the entire read-modify-write operation. The callback
// must mutate its argument and return; it must not call any state/file methods.
func (s *Store) UpdateState(ctx context.Context, owner, id string, fn func(map[string]any) error) error {
	s.fsMu.Lock()
	defer s.fsMu.Unlock()
	b, err := s.loadStateLocked(ctx, owner, id)
	if err != nil {
		return err
	}
	var state map[string]any
	if err = json.Unmarshal(b, &state); err != nil || state == nil {
		return ErrInvalid
	}
	if err = fn(state); err != nil {
		return err
	}
	b, err = json.Marshal(state)
	if err != nil {
		return err
	}
	if len(b) > 32<<20 {
		return ErrInvalid
	}
	return s.writeStateLocked(ctx, owner, id, b)
}
func stateMap(v any) map[string]any { m, _ := v.(map[string]any); return m }
func stateArray(v any) []any        { a, _ := v.([]any); return a }
func text(v any) string             { s, _ := v.(string); return s }
func imageTime(m map[string]any) int64 {
	switch v := m["imageUpdatedAt"].(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	case string:
		if n, e := strconv.ParseInt(v, 10, 64); e == nil {
			return n
		}
		if t, e := time.Parse(time.RFC3339Nano, v); e == nil {
			return t.UnixMilli()
		}
	}
	return 0
}

var imageFields = []string{"page_image_url", "pageImageUrl", "imagePath", "referenceImagePath", "refImagePath", "imageUrl", "image_url", "imageHistory", "image_history", "pageImageHistory", "imageUpdatedAt", "status"}

func hasImage(m map[string]any) bool {
	for _, k := range imageFields {
		if k == "imageUpdatedAt" || k == "status" || k == "imageHistory" || k == "image_history" || k == "pageImageHistory" {
			continue
		}
		if text(m[k]) != "" {
			return true
		}
	}
	return false
}

// Existing nonempty images win over empty/stale client snapshots. A client can
// select a new image only by supplying a strictly newer imageUpdatedAt. The
// internal generation writer uses UpdateState and owns that timestamp.
func mergeImage(old, next map[string]any) {
	if old == nil || next == nil || !hasImage(old) {
		return
	}
	if hasImage(next) && imageTime(next) > imageTime(old) {
		return
	}
	for _, k := range imageFields {
		if v, ok := old[k]; ok {
			next[k] = v
		} else if k != "status" {
			delete(next, k)
		}
	}
}
func keyedMerge(old, next []any, key string, fn func(map[string]any, map[string]any)) {
	lookup := map[string]map[string]any{}
	for _, v := range old {
		m := stateMap(v)
		if m == nil || m[key] == nil {
			continue
		}
		lookup[fmt.Sprint(m[key])] = m
	}
	for _, v := range next {
		m := stateMap(v)
		if m == nil || m[key] == nil {
			continue
		}
		if prev := lookup[fmt.Sprint(m[key])]; prev != nil {
			fn(prev, m)
		}
	}
}
func mergeRoot(old, next map[string]any) {
	if old == nil || next == nil {
		return
	}
	keyedMerge(stateArray(old["pages"]), stateArray(next["pages"]), "page_index", mergeImage)
	for _, lib := range []string{"characterLibrary", "sceneLibrary"} {
		keyedMerge(stateArray(old[lib]), stateArray(next[lib]), "name", mergeImage)
	}
}
func episodeKey(m map[string]any) string {
	for _, k := range []string{"id", "episodeId", "episode_index", "index", "name", "title"} {
		if m[k] != nil && fmt.Sprint(m[k]) != "" {
			return k + ":" + fmt.Sprint(m[k])
		}
	}
	return ""
}
func preserveImages(old, next map[string]any) {
	mergeRoot(old, next)
	mergeRoot(stateMap(old["storyboard"]), stateMap(next["storyboard"]))
	lookup := map[string]map[string]any{}
	for _, v := range stateArray(old["episodes"]) {
		m := stateMap(v)
		if k := episodeKey(m); k != "" {
			lookup[k] = m
		}
	}
	for _, v := range stateArray(next["episodes"]) {
		m := stateMap(v)
		prev := lookup[episodeKey(m)]
		if prev != nil {
			mergeRoot(prev, m)
			mergeRoot(stateMap(prev["storyboard"]), stateMap(m["storyboard"]))
		}
	}
}
