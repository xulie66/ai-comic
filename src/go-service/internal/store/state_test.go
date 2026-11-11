package store

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
)

func TestStaleStatePreservesGeneratedImages(t *testing.T) {
	ctx := context.Background()
	s, e := Open(ctx, Options{DataRoot: t.TempDir()})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	_ = s.CreateUser(ctx, User{ID: "u", Username: "u"})
	p, e := s.CreateProject(ctx, "u", "book")
	if e != nil {
		t.Fatal(e)
	}
	initial := json.RawMessage(`{"storyboard":{"pages":[{"page_index":1,"page_image_url":"old.png"}]},"characterLibrary":[{"name":"A","referenceImagePath":"old-character.png"}],"episodes":[{"id":"ep1","storyboard":{"pages":[{"page_index":1,"page_image_url":"episode-old.png"}]}},{"id":"ep2","storyboard":{"pages":[{"page_index":1,"page_image_url":"other-episode.png"}]}}]}`)
	if e = s.SaveState(ctx, "u", p.ID, initial); e != nil {
		t.Fatal(e)
	}
	if e = s.UpdateState(ctx, "u", p.ID, func(m map[string]any) error {
		page := stateMap(stateArray(stateMap(m["storyboard"])["pages"])[0])
		page["page_image_url"] = "new.png"
		page["imageUpdatedAt"] = 200.0
		card := stateMap(stateArray(m["characterLibrary"])[0])
		card["referenceImagePath"] = "new-character.png"
		card["imageUpdatedAt"] = 200.0
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	if e = s.SaveState(ctx, "u", p.ID, initial); e != nil {
		t.Fatal(e)
	}
	b, e := s.LoadState(ctx, "u", p.ID)
	if e != nil {
		t.Fatal(e)
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	page := stateMap(stateArray(stateMap(m["storyboard"])["pages"])[0])
	if page["page_image_url"] != "new.png" {
		t.Fatal("stale nonempty path overwrote new image", string(b))
	}
	card := stateMap(stateArray(m["characterLibrary"])[0])
	if card["referenceImagePath"] != "new-character.png" {
		t.Fatal("character lost", string(b))
	}
	empty := json.RawMessage(`{"storyboard":{"pages":[{"page_index":1,"page_image_url":""}]},"episodes":[{"id":"ep2","storyboard":{"pages":[{"page_index":1,"page_image_url":""}]}},{"id":"ep1","storyboard":{"pages":[{"page_index":1,"page_image_url":""}]}}]}`)
	if e = s.SaveState(ctx, "u", p.ID, empty); e != nil {
		t.Fatal(e)
	}
	b, _ = s.LoadState(ctx, "u", p.ID)
	_ = json.Unmarshal(b, &m)
	eps := stateArray(m["episodes"])
	ep2page := stateMap(stateArray(stateMap(stateMap(eps[0])["storyboard"])["pages"])[0])
	if ep2page["page_image_url"] != "other-episode.png" {
		t.Fatal("episode image crossed boundaries", string(b))
	}
	newer := json.RawMessage(`{"storyboard":{"pages":[{"page_index":1,"page_image_url":"chosen.png","imageUpdatedAt":300}]}}`)
	if e = s.SaveState(ctx, "u", p.ID, newer); e != nil {
		t.Fatal(e)
	}
	b, _ = s.LoadState(ctx, "u", p.ID)
	_ = json.Unmarshal(b, &m)
	page = stateMap(stateArray(stateMap(m["storyboard"])["pages"])[0])
	if page["page_image_url"] != "chosen.png" {
		t.Fatal("newer choice not accepted", string(b))
	}
}
func TestConcurrentAtomicUpdates(t *testing.T) {
	ctx := context.Background()
	s, e := Open(ctx, Options{DataRoot: t.TempDir()})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	_ = s.CreateUser(ctx, User{ID: "u", Username: "u"})
	p, e := s.CreateProject(ctx, "u", "book")
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	for i := 0; i < 15; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := s.UpdateState(ctx, "u", p.ID, func(m map[string]any) error { m[fmt.Sprint(i)] = i; return nil }); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	b, _ := s.LoadState(ctx, "u", p.ID)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if len(m) != 15 {
		t.Fatal("lost atomic updates", string(b))
	}
}
