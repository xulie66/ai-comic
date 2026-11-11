package httpapi

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"comic.local/go-service/internal/store"
)

func TestGenerationReferencesBindingsAndIsolation(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, store.Options{DataRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for _, id := range []string{"u", "other"} {
		if err = st.CreateUser(ctx, store.User{ID: id, Username: id}); err != nil {
			t.Fatal(err)
		}
	}
	p, err := st.CreateProject(ctx, "u", "fixture")
	if err != nil {
		t.Fatal(err)
	}
	other, err := st.CreateProject(ctx, "other", "private")
	if err != nil {
		t.Fatal(err)
	}
	png := []byte{137, 80, 78, 71, 13, 10, 26, 10, 0, 0, 0, 0}
	image := func(name string) string {
		t.Helper()
		file, e := st.WriteImage(ctx, "u", p.ID, "references/"+name+".png", png)
		if e != nil {
			t.Fatal(e)
		}
		return file
	}
	alice, bob, room, unrelated := image("alice"), image("bob"), image("room"), image("unrelated")
	state := map[string]any{
		"characterLibrary": []any{map[string]any{"name": "Alice", "canonical_name": "爱丽丝", "aliases": []string{"阿丽"}, "role": "female_lead", "referenceImagePath": alice}},
		"storyboard":       map[string]any{"characterLibrary": []any{map[string]any{"name": "Bob", "referenceImagePath": bob}}, "maleLead": map[string]any{"name": "Alice duplicate", "referenceImagePath": alice}, "sceneLibrary": []any{map[string]any{"name": "unrelated", "refImagePath": unrelated}}},
		"canonical":        map[string]any{"sceneLibrary": []any{map[string]any{"name": "Room", "aliases": []string{"卧室"}, "refImagePath": room}}},
	}
	api := &Server{Store: st}
	refs, err := api.generationReferences(ctx, "u", p.ID, "page", map[string]any{"page": map[string]any{"scene": "卧室", "panels": []any{map[string]any{"characters": []string{"阿丽"}}}}}, state)
	if err != nil || len(refs) != 2 || refs[0].Type != "scene" || refs[1].Name != "爱丽丝" || refs[0].MIME != "image/png" {
		t.Fatalf("page refs %+v err %v", refs, err)
	}
	refs, err = api.generationReferences(ctx, "u", p.ID, "scene", map[string]any{"scene": map[string]any{"characters": []string{"Bob"}}}, state)
	if err != nil || len(refs) != 1 || refs[0].Name != "Bob" {
		t.Fatalf("scene refs %+v %v", refs, err)
	}
	refs, err = api.generationReferences(ctx, "u", p.ID, "scene", map[string]any{"scene": map[string]any{"name": "Room"}}, state)
	if err != nil || len(refs) != 0 {
		t.Fatalf("unbound scene %+v %v", refs, err)
	}
	object(state["canonical"])["sceneLibrary"] = []any{map[string]any{"name": "Bound room", "characters": []string{"Bob"}}}
	refs, err = api.generationReferences(ctx, "u", p.ID, "scene", map[string]any{"scene": map[string]any{"name": "Bound room"}}, state)
	if err != nil || len(refs) != 1 || refs[0].Name != "Bob" {
		t.Fatalf("library binding %+v %v", refs, err)
	}
	refs, err = api.generationReferences(ctx, "u", p.ID, "character", map[string]any{"character": map[string]any{"name": "Bob"}}, state)
	if err != nil || len(refs) != 0 {
		t.Fatalf("character must not inherit other refs %+v %v", refs, err)
	}
	refs, err = api.generationReferences(ctx, "u", p.ID, "character", map[string]any{"character": map[string]any{"name": "Bob", "referenceImagePath": bob}}, state)
	if err != nil || len(refs) != 1 {
		t.Fatalf("self ref %+v %v", refs, err)
	}
	if _, err = api.generationReferences(ctx, "u", other.ID, "page", map[string]any{}, state); err == nil {
		t.Fatal("another owner's project accepted")
	}
	_, err = api.generationReferences(ctx, "u", p.ID, "character", map[string]any{"character": map[string]any{"referenceImagePath": "/data/projects/" + other.ID + "/references/private.png"}}, state)
	if !errors.Is(err, store.ErrForbidden) {
		t.Fatalf("cross-project path: %v", err)
	}
}

func TestGenerationReferencesFallbackLimitAndDedup(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, store.Options{DataRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err = st.CreateUser(ctx, store.User{ID: "u", Username: "user"}); err != nil {
		t.Fatal(err)
	}
	p, err := st.CreateProject(ctx, "u", "fixture")
	if err != nil {
		t.Fatal(err)
	}
	cards := []any{}
	png := []byte{137, 80, 78, 71, 13, 10, 26, 10}
	for i := 0; i < 20; i++ {
		file, e := st.WriteImage(ctx, "u", p.ID, fmt.Sprintf("references/%d.png", i), png)
		if e != nil {
			t.Fatal(e)
		}
		role := "supporting"
		if i == 19 {
			role = "male_lead"
		}
		cards = append(cards, map[string]any{"name": fmt.Sprint(i), "role": role, "referenceImagePath": file})
	}
	api := &Server{Store: st}
	refs, err := api.generationReferences(ctx, "u", p.ID, "page", map[string]any{}, map[string]any{"characterLibrary": cards, "storyboard": map[string]any{"characterLibrary": cards}})
	if err != nil || len(refs) != 16 || refs[0].Name != "19" {
		t.Fatalf("fallback count/order %d %+v %v", len(refs), refs, err)
	}
	refs, err = api.generationReferences(ctx, "u", p.ID, "page", map[string]any{"page": map[string]any{"characters": []string{"missing"}}}, map[string]any{"characterLibrary": cards})
	if err != nil || len(refs) != 0 {
		t.Fatalf("explicit unknown must not fallback %+v %v", refs, err)
	}
}
