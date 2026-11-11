package generation

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func fixtureDocument(source string) map[string]any {
	return map[string]any{"title": "测试", "characterLibrary": []any{map[string]any{"name": "林红", "appearance": "短发"}, map[string]any{"name": "林紅", "appearance": "重复"}}, "pages": []any{map[string]any{"page_number": 99, "original_text_range": source, "panels": []any{map[string]any{"visual_prompt": "林红走路", "dialogue_text": []any{map[string]any{"text": "你好"}}}}}}}
}
func respondCompletion(w http.ResponseWriter, value any) {
	b, _ := json.Marshal(value)
	_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": string(b)}}}, "usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "prompt_tokens_details": map[string]any{"cached_tokens": 2}}})
}
func TestBuildStoryboardChunksMergeReuse(t *testing.T) {
	text := strings.Repeat("林红走到门边。\n", 620)
	var mu sync.Mutex
	active, maxActive, calls := 0, 0, 0
	allCanonical := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		prompt := str(obj(list(req["messages"])[0])["content"])
		parts := strings.SplitN(prompt, "【当前正文，必须完整处理】：\n", 2)
		if len(parts) != 2 {
			t.Error("missing source marker")
			return
		}
		source := strings.SplitN(parts[1], "\n【后文上下文，仅供理解，不生成】", 2)[0]
		mu.Lock()
		calls++
		call := calls
		active++
		if active > maxActive {
			maxActive = active
		}
		if call > 1 && !strings.Contains(prompt, "全书固定角色档案") {
			allCanonical = false
		}
		mu.Unlock()
		if call > 1 {
			time.Sleep(15 * time.Millisecond)
		}
		respondCompletion(w, fixtureDocument(source))
		mu.Lock()
		active--
		mu.Unlock()
	}))
	defer srv.Close()
	c := Client{HTTP: srv.Client()}
	out, u, err := c.BuildStoryboard(context.Background(), Config{Endpoint: srv.URL}, map[string]any{"originalText": text, "storyboardChunkSize": 1000, "pageCount": 12})
	if err != nil {
		t.Fatal(err)
	}
	chunks := storyChunks(map[string]any{"originalText": text, "storyboardChunkSize": 1000, "pageCount": 12})
	if calls != len(chunks) || maxActive > 3 || maxActive < 2 || !allCanonical {
		t.Fatalf("calls=%d chunks=%d concurrency=%d canonical=%t", calls, len(chunks), maxActive, allCanonical)
	}
	if u != (Usage{int64(calls * 10), int64(calls * 5), int64(calls * 2)}) {
		t.Fatalf("usage %+v", u)
	}
	if len(list(out["characterLibrary"])) != 1 {
		t.Fatal("characters not deduplicated")
	}
	var source strings.Builder
	for i, v := range list(out["pages"]) {
		page := obj(v)
		if page["page_number"] != i+1 || page["page_index"] != i+1 {
			t.Fatal("page order")
		}
		source.WriteString(str(page["original_text_range"]))
	}
	if source.String() != text {
		t.Fatal("source omitted or reordered")
	}
	pageTargets := 0
	for _, ch := range chunks {
		pageTargets += ch.pages
	}
	if pageTargets != 12 {
		t.Fatalf("page allocation %d", pageTargets)
	}
}
func TestStoryboardInvalidSchemaRejected(t *testing.T) {
	for _, bad := range []map[string]any{{"pages": []any{}}, {"pages": "wrong"}, {"pages": []any{map[string]any{"original_text_range": "原文", "panels": []any{map[string]any{"visual_prompt": 42}}}}}, fixtureDocument("漏段")} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { respondCompletion(w, bad) }))
		c := Client{HTTP: srv.Client()}
		_, u, err := c.BuildStoryboard(context.Background(), Config{Endpoint: srv.URL}, map[string]any{"originalText": "完整原文"})
		if !errors.Is(err, ErrResponse) || u.InputTokens != 10 {
			t.Fatalf("schema %v usage %+v err %v", bad, u, err)
		}
		srv.Close()
	}
}
func TestTraditionalSourcePreserved(t *testing.T) {
	source := "她說：歡迎回來。"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		prompt := str(obj(list(req["messages"])[0])["content"])
		if !strings.Contains(prompt, "【当前正文，必须完整处理】：\n"+source) {
			t.Error("source converted before coverage validation")
		}
		respondCompletion(w, fixtureDocument(source))
	}))
	defer srv.Close()
	c := Client{HTTP: srv.Client()}
	_, _, err := c.BuildStoryboard(context.Background(), Config{Endpoint: srv.URL}, map[string]any{"originalText": source})
	if err != nil {
		t.Fatal(err)
	}
}
func TestEpisodeSplittingPreservesSource(t *testing.T) {
	chunks := storyChunks(map[string]any{"episodes": []any{map[string]any{"text": "第一集\n原文"}, map[string]any{"originalText": "第二集\n后文"}}})
	if len(chunks) != 2 || chunks[0].episode != 1 || chunks[1].episode != 2 || chunks[1].text != "第二集\n后文" {
		t.Fatalf("episodes %+v", chunks)
	}
}
func TestAssetsObjectArrayAndSchema(t *testing.T) {
	for _, value := range []any{[]any{map[string]any{"name": "小红"}}, map[string]any{"items": []any{map[string]any{"name": "小红"}}}, map[string]any{"characters": []any{map[string]any{"name": "小红"}}}, map[string]any{"items": []any{"wrong"}}} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { respondCompletion(w, value) }))
		c := Client{HTTP: srv.Client()}
		out, u, err := c.Assets(context.Background(), Config{Endpoint: srv.URL}, "characters", "小红走了")
		bad := false
		if m, ok := value.(map[string]any); ok {
			bad = len(list(m["items"])) > 0 && list(m["items"])[0] == "wrong"
		}
		if bad {
			if !errors.Is(err, ErrResponse) {
				t.Fatalf("accepted bad asset: %v", err)
			}
		} else if err != nil || len(list(out["items"])) != 1 || u.InputTokens != 10 {
			t.Fatalf("%v %+v %v", out, u, err)
		}
		srv.Close()
	}
}
