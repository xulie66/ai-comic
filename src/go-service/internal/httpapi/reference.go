package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"comic.local/go-service/internal/generation"
	"comic.local/go-service/internal/store"
)

// generationReferences selects only references relevant to this generation. Every
// path is resolved through the tenant store, including browser-supplied paths.
func (s *Server) generationReferences(ctx context.Context, uid, pid, kind string, payload, state map[string]any) ([]generation.Reference, error) {
	if _, err := s.Store.ProjectRoot(ctx, uid, pid); err != nil {
		return nil, err
	}
	roots := []map[string]any{state, object(state["storyboard"]), object(state["canonical"]), object(object(state["storyboard"])["canonical"]), object(payload["storyboard"])}
	chars, scenes := []map[string]any{}, []map[string]any{}
	for _, root := range roots {
		chars = append(chars, referenceCards(root["characterLibrary"])...)
		scenes = append(scenes, referenceCards(root["sceneLibrary"])...)
		for _, key := range []string{"maleLead", "femaleLead"} {
			card := object(root[key])
			if len(card) > 0 {
				copy := map[string]any{}
				for k, v := range card {
					copy[k] = v
				}
				if str(copy, "role") == "" {
					if key == "maleLead" {
						copy["role"] = "male_lead"
					} else {
						copy["role"] = "female_lead"
					}
				}
				chars = append(chars, copy)
			}
		}
		chars = append(chars, referenceCards(root["supportingCharacters"])...)
	}
	out := []generation.Reference{}
	seen := map[string]bool{}
	add := func(card map[string]any, typ string) error {
		if len(out) >= 16 {
			return nil
		}
		name := ""
		for _, key := range []string{"referenceImagePath", "refImagePath", "imagePath", "image_url", "imageUrl"} {
			if name = str(card, key); name != "" {
				break
			}
		}
		if name == "" {
			return nil
		}
		absolute, err := s.Store.ImagePath(ctx, uid, pid, name)
		if err != nil {
			return err
		}
		if seen[absolute] {
			return nil
		}
		b, err := s.Store.ReadImage(ctx, uid, pid, name)
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		mime := http.DetectContentType(b)
		if !strings.HasPrefix(mime, "image/") {
			return nil
		}
		seen[absolute] = true
		label := str(card, "canonical_name")
		if label == "" {
			label = str(card, "name")
		}
		out = append(out, generation.Reference{Name: label, Type: typ, Data: b, MIME: mime})
		return nil
	}
	matching := func(cards []map[string]any, names []string, typ string) error {
		for _, name := range names {
			for _, card := range cards {
				if referenceMatches(card, name) {
					if err := add(card, typ); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}
	if kind == "character" {
		if err := add(object(payload["character"]), "character"); err != nil {
			return nil, err
		}
		return out, nil
	}
	if kind == "scene" {
		scene := object(payload["scene"])
		names := []string{}
		bound := []map[string]any{scene}
		for _, candidate := range scenes {
			for _, label := range referenceNames(scene) {
				if referenceMatches(candidate, label) {
					bound = append(bound, candidate)
					break
				}
			}
		}
		for _, card := range bound {
			for _, key := range []string{"characters", "character_ids", "boundCharacters", "boundCharacterNames"} {
				names = append(names, referenceNames(card[key])...)
			}
		}
		if err := matching(chars, names, "character"); err != nil {
			return nil, err
		}
		return out, nil
	}
	page := object(payload["page"])
	sceneNames := referenceNames(page["scene"])
	characterNames := referenceNames(page["characters"])
	characterNames = append(characterNames, referenceNames(page["character_ids"])...)
	for _, panel := range referenceCards(page["panels"]) {
		sceneNames = append(sceneNames, referenceNames(panel["scene"])...)
		characterNames = append(characterNames, referenceNames(panel["characters"])...)
		characterNames = append(characterNames, referenceNames(panel["chars"])...)
	}
	if err := matching(scenes, sceneNames, "scene"); err != nil {
		return nil, err
	}
	if len(characterNames) > 0 {
		if err := matching(chars, characterNames, "character"); err != nil {
			return nil, err
		}
	} else {
		// Fallback preserves legacy projects without panel character annotations.
		for _, lead := range []bool{true, false} {
			for _, card := range chars {
				role := str(card, "role")
				isLead := role == "male_lead" || role == "female_lead" || role == "maleLead" || role == "femaleLead"
				if isLead == lead {
					if err := add(card, "character"); err != nil {
						return nil, err
					}
				}
			}
		}
	}
	return out, nil
}

func referenceCards(v any) []map[string]any {
	out := []map[string]any{}
	switch list := v.(type) {
	case []any:
		for _, item := range list {
			if card, ok := item.(map[string]any); ok {
				out = append(out, card)
			}
		}
	case []map[string]any:
		out = append(out, list...)
	case map[string]any:
		out = append(out, list)
	}
	return out
}
func referenceNames(v any) []string {
	out := []string{}
	switch x := v.(type) {
	case string:
		for _, part := range strings.FieldsFunc(x, func(r rune) bool { return r == ',' || r == '，' || r == '、' || r == '\n' || r == ';' || r == '；' }) {
			if name := strings.TrimSpace(part); name != "" {
				out = append(out, name)
			}
		}
	case []any:
		for _, item := range x {
			out = append(out, referenceNames(item)...)
		}
	case []string:
		for _, item := range x {
			out = append(out, referenceNames(item)...)
		}
	case map[string]any:
		for _, key := range []string{"canonical_name", "name", "character_id", "id", "scene_id"} {
			if name, ok := x[key].(string); ok && name != "" {
				out = append(out, name)
			}
		}
	}
	return out
}
func referenceMatches(card map[string]any, name string) bool {
	names := referenceNames(card)
	names = append(names, referenceNames(card["aliases"])...)
	for _, candidate := range names {
		if strings.EqualFold(strings.TrimSpace(candidate), strings.TrimSpace(name)) {
			return true
		}
	}
	return false
}
