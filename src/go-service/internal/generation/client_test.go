package generation

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTextProtocols(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/chat/completions" {
					t.Errorf("path %s", r.URL.Path)
				}
				if r.Header.Get("Authorization") != "Bearer local-test" {
					t.Error("missing auth")
				}
				var req map[string]any
				_ = json.NewDecoder(r.Body).Decode(&req)
				if req["stream"] != true || req["model"] != "test-model" {
					t.Errorf("request %v", req)
				}
				if stream {
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprint(w, ": ping\r\n\r\ndata: {\"choices\":[{\"delta\":{\"reasoning_content\":\"ignore\"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"```json\\n{\\\"pages\\\":\"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"[]}\\n```\"}}]}\n\ndata: [DONE]")
				} else {
					_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": "Here is JSON: ```json\n{\"pages\":[]}\n```"}}}})
				}
			}))
			defer srv.Close()
			c := Client{HTTP: srv.Client()}
			raw, e := c.Text(context.Background(), Config{Endpoint: srv.URL + "/v1", APIKey: "local-test", Model: "test-model"}, "prompt")
			if e != nil || string(raw) != `{"pages":[]}` {
				t.Fatalf("%s %v", raw, e)
			}
		})
	}
}
func TestImageProtocols(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\nlocal fixture")
	for _, kind := range []string{"generations-base64", "chat-data", "chat-url", "chat-block"} {
		t.Run(kind, func(t *testing.T) {
			var srv *httptest.Server
			srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/asset.png" {
					if r.Header.Get("Authorization") != "" {
						t.Error("API key leaked to result URL")
					}
					w.Header().Set("Content-Type", "image/png")
					w.Write(png)
					return
				}
				var req map[string]any
				if e := json.NewDecoder(r.Body).Decode(&req); e != nil {
					t.Fatal(e)
				}
				uri := "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
				if strings.HasPrefix(kind, "chat") {
					messages := list(req["messages"])
					content := list(obj(messages[0])["content"])
					if len(content) != 2 || obj(obj(content[1])["image_url"])["url"] != uri {
						t.Errorf("reference blocks %v", content)
					}
					if !strings.Contains(str(obj(content[0])["text"]), "ZH_CN_TEXT_V2") {
						t.Error("missing simplified guard")
					}
				} else {
					if list(req["image_urls"])[0] != uri {
						t.Error("reference image missing")
					}
					if req["size"] != "3:4" {
						t.Error("aspect mismatch")
					}
				}
				switch kind {
				case "generations-base64":
					fmt.Fprintf(w, `{"data":[{"b64_json":%q}]}`, base64.StdEncoding.EncodeToString(png))
				case "chat-data":
					fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}]}`, uri)
				case "chat-url":
					fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}]}`, "![image]("+srv.URL+"/asset.png)")
				case "chat-block":
					fmt.Fprintf(w, `{"choices":[{"message":{"content":[{"type":"image_url","image_url":{"url":%q}}]}}]}`, uri)
				}
			}))
			defer srv.Close()
			endpoint := srv.URL + "/v1/images/generations"
			if strings.HasPrefix(kind, "chat") {
				endpoint = srv.URL + "/v1/chat/completions"
			}
			c := Client{HTTP: srv.Client()}
			b, mt, e := c.Image(context.Background(), Config{Endpoint: endpoint, APIKey: "secret", Model: "image-model"}, "畫圖", []Reference{{Name: "角色", Type: "characters", Data: png, MIME: "image/png"}}, "3:4")
			if e != nil || mt != "image/png" || !bytes.Equal(b, png) {
				t.Fatalf("%q %s %v", b, mt, e)
			}
		})
	}
}
func TestErrorsDoNotLeak(t *testing.T) {
	var logs bytes.Buffer
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "provider-secret diagnostic", 502) }))
	defer srv.Close()
	c := Client{HTTP: srv.Client(), Logger: slog.New(slog.NewTextHandler(&logs, nil))}
	_, e := c.Text(context.Background(), Config{Endpoint: srv.URL}, "x")
	if !errors.Is(e, ErrGeneration) || strings.Contains(e.Error(), "provider-secret") {
		t.Fatalf("public error %v", e)
	}
	if !strings.Contains(logs.String(), "provider-secret diagnostic") {
		t.Error("internal detail missing")
	}
}
func TestTimeoutAndCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(100 * time.Millisecond):
		}
	}))
	defer srv.Close()
	c := Client{HTTP: srv.Client(), Logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))}
	_, e := c.Text(context.Background(), Config{Endpoint: srv.URL, Timeout: 20 * time.Millisecond}, "x")
	if !errors.Is(e, ErrTimeout) {
		t.Fatalf("timeout %v", e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, e = c.Text(ctx, Config{Endpoint: srv.URL}, "x")
	if !errors.Is(e, ErrCanceled) {
		t.Fatalf("cancel %v", e)
	}
}
func TestInvalidResponses(t *testing.T) {
	for _, response := range []string{`{"choices":[{"message":{"content":"not JSON"}}]}`, `{"choices":[{"message":{"content":""}}]}`, `{"error":{"message":"private"}}`} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, response) }))
		c := Client{HTTP: srv.Client(), Logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))}
		_, e := c.Text(context.Background(), Config{Endpoint: srv.URL}, "x")
		if !errors.Is(e, ErrResponse) {
			t.Errorf("%s: %v", response, e)
		}
		srv.Close()
	}
}
func TestPromptLocksAndUIFields(t *testing.T) {
	p := map[string]any{"aspectRatio": "3:4", "artStyle": "matchstick", "pageCharacterLock": "女主紅髮", "pageSceneLock": "門在左側", "page": map[string]any{"promptOverride": "改構圖", "panels": []any{map[string]any{"dialogue_text": []any{map[string]any{"text": "歡迎回來"}, map[string]any{"content": "你說什麼"}}, "narration_text": "場景中的話", "phone_text": []any{"發送訊息"}}}}}
	got := PagePrompt(p)
	for _, s := range []string{"逐格最终上字清单", "欢迎回来", "你说什么", "发送讯息", "女主红发", "门在左侧", "火柴人", "改构图"} {
		if !strings.Contains(got, s) {
			t.Errorf("missing %s", s)
		}
	}
	if strings.Contains(got, "map[") {
		t.Error("object dialogue stringified")
	}
	if strings.LastIndex(got, "门在左侧") < strings.Index(got, "改构图") {
		t.Error("override outranks scene lock")
	}
	for _, fn := range []func(map[string]any) string{CharacterPrompt, ScenePrompt} {
		s := fn(map[string]any{"promptOverride": "自己畫", "character": map[string]any{"name": "小紅", "appearance": "短髮"}, "scene": map[string]any{"description": "門在左邊"}})
		if !strings.Contains(s, "自己画") || !strings.Contains(s, "锁定") {
			t.Error("override must preserve locks")
		}
	}
}
func TestStoryboardPrompt(t *testing.T) {
	s := StoryboardPrompt(map[string]any{"originalText": "她說：歡迎。", "targetPageCount": 3, "canonicalCharacters": []any{map[string]any{"name": "小紅"}}, "aspectRatio": "3:4"})
	for _, v := range []string{"她说：欢迎。", "目标漫画页数：3", "小红", "characterLibrary 输出空数组", "dialogue_text", "phone_text"} {
		if !strings.Contains(s, v) {
			t.Errorf("missing %s", v)
		}
	}
}
