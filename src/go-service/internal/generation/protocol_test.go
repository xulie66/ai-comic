package generation

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestImageBaseURLProtocolAndCompletePathPrecedence(t *testing.T) {
	for _, tc := range []struct{ protocol, path, want string }{
		{"chat", "/v1", "/v1/chat/completions"},
		{"openai-chat", "/v1/", "/v1/chat/completions"},
		{"chat-completions", "/v1", "/v1/chat/completions"},
		{"images", "/v1", "/v1/images/generations"},
		{"chat", "/v1/images/generations", "/v1/images/generations"},
		{"images", "/v1/chat/completions", "/v1/chat/completions"},
	} {
		t.Run(tc.protocol+tc.path, func(t *testing.T) {
			ref := []byte("\x89PNG\r\n\x1a\nreference")
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tc.want {
					t.Errorf("path got %s want %s", r.URL.Path, tc.want)
				}
				var req map[string]any
				_ = json.NewDecoder(r.Body).Decode(&req)
				if tc.want == "/v1/chat/completions" {
					messages := list(req["messages"])
					if len(messages) != 1 {
						t.Error("chat message missing")
						return
					}
					blocks := list(obj(messages[0])["content"])
					if len(blocks) != 2 {
						t.Error("reference blocks missing")
						return
					}
					if obj(obj(blocks[1])["image_url"])["url"] != "data:image/png;base64,"+base64.StdEncoding.EncodeToString(ref) {
						t.Error("reference data URI mismatch")
					}
					if _, ok := req["prompt"]; ok {
						t.Error("chat sent generations body")
					}
				} else if len(list(req["image_urls"])) != 1 {
					t.Error("generations reference missing")
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"b64_json": base64.StdEncoding.EncodeToString(ref)}}})
			}))
			defer srv.Close()
			c := Client{HTTP: srv.Client()}
			_, _, _, err := c.ImageDetailed(context.Background(), Config{Endpoint: srv.URL + tc.path, Protocol: tc.protocol}, "生成漫画", []Reference{{Data: ref, MIME: "image/png"}}, "9:16")
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
