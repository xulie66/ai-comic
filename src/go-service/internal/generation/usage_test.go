package generation

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDetailedTextUsage(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req map[string]any
				_ = json.NewDecoder(r.Body).Decode(&req)
				if obj(req["stream_options"])["include_usage"] != true {
					t.Error("usage not requested")
				}
				packet := map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": `{"pages":[]}`}}}, "usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 30, "prompt_tokens_details": map[string]any{"cached_tokens": 20}}}
				if stream {
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"{\\\"pages\\\":[]}\"}}]}\n\n")
					packet["choices"] = []any{}
					b, _ := json.Marshal(packet)
					fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n", b)
				} else {
					_ = json.NewEncoder(w).Encode(packet)
				}
			}))
			defer srv.Close()
			c := Client{HTTP: srv.Client()}
			raw, u, err := c.TextDetailed(context.Background(), Config{Endpoint: srv.URL}, "x")
			if err != nil || string(raw) != `{"pages":[]}` || u != (Usage{100, 30, 20}) {
				t.Fatalf("raw=%s usage=%+v err=%v", raw, u, err)
			}
		})
	}
}
func TestAnthropicCacheInputSemantics(t *testing.T) {
	for _, tc := range []struct {
		body string
		want Usage
	}{
		{`{"usage":{"input_tokens":10,"output_tokens":3,"cache_read_input_tokens":20,"cache_creation_input_tokens":5}}`, Usage{35, 3, 20}},
		{`{"usage":{"input_tokens":10,"output_tokens":3,"cache_creation_input_tokens":5}}`, Usage{15, 3, 0}},
		{`{"usage":{"input_tokens":10,"output_tokens":3,"cache_read_input_tokens":20}}`, Usage{30, 3, 20}},
		{`{"usage":{"prompt_tokens":100,"completion_tokens":3,"cache_read_input_tokens":20,"cache_creation_input_tokens":5,"prompt_tokens_details":{"cached_tokens":20}}}`, Usage{100, 3, 20}},
		{`{"usage":{"input_tokens":100,"output_tokens":3,"input_tokens_details":{"cached_tokens":20}}}`, Usage{100, 3, 20}},
	} {
		var packet any
		_ = json.Unmarshal([]byte(tc.body), &packet)
		if got := parseUsage(packet); got != tc.want {
			t.Errorf("%s got %+v want %+v", tc.body, got, tc.want)
		}
	}
}
func TestProviderUsageShapes(t *testing.T) {
	for _, tc := range []struct {
		body string
		want Usage
	}{{`{"usage":{"input_tokens":12,"output_tokens":3,"input_tokens_details":{"cached_tokens":4}}}`, Usage{12, 3, 4}}, {`{"usage":{"prompt_tokens":12,"completion_tokens":3,"prompt_cache_hit_tokens":4}}`, Usage{12, 3, 4}}, {`{"usageMetadata":{"promptTokenCount":12,"candidatesTokenCount":3,"cachedContentTokenCount":4}}`, Usage{12, 3, 4}}, {`{"data":[{"usage":{"prompt_tokens":12,"completion_tokens":3}}]}`, Usage{12, 3, 0}}, {`{}`, Usage{}}} {
		var packet any
		_ = json.Unmarshal([]byte(tc.body), &packet)
		if got := parseUsage(packet); got != tc.want {
			t.Errorf("%s: %+v", tc.body, got)
		}
	}
}
func TestImageAsyncAndUsage(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		t.Run(fmt.Sprint(explicit), func(t *testing.T) {
			png := []byte("\x89PNG\r\n\x1a\nfixture")
			count := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					if explicit {
						fmt.Fprint(w, `{"task_id":"fixture","statusUrl":"/custom-status","usage":{"input_tokens":40}}`)
					} else {
						fmt.Fprint(w, `{"data":[{"task_id":"fixture"}],"usage":{"input_tokens":40}}`)
					}
					return
				}
				expected := "/v1/images/tasks/fixture"
				if explicit {
					expected = "/custom-status"
				}
				if r.URL.Path != expected {
					t.Errorf("unexpected poll path %s", r.URL.Path)
				}
				if r.Header.Get("Authorization") != "Bearer local" {
					t.Error("poll missing auth")
				}
				count++
				fmt.Fprintf(w, `{"status":"completed","data":[{"b64_json":%q}],"usage":{"input_tokens":40,"output_tokens":25,"input_tokens_details":{"cached_tokens":5}}}`, base64.StdEncoding.EncodeToString(png))
			}))
			defer srv.Close()
			c := Client{HTTP: srv.Client()}
			b, mt, u, e := c.ImageDetailed(context.Background(), Config{Endpoint: srv.URL + "/v1/images/generations", APIKey: "local"}, "x", nil, "1:1")
			if e != nil || !bytes.Equal(b, png) || mt != "image/png" || u != (Usage{40, 25, 5}) || count != 1 {
				t.Fatalf("%q %s %+v polls=%d err=%v", b, mt, u, count, e)
			}
		})
	}
}
func TestAsyncCancellationAndFailure(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(fmt.Sprint(failure), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					fmt.Fprint(w, `{"task_id":"fixture"}`)
					return
				}
				if failure {
					fmt.Fprint(w, `{"status":"failed","error":"private-provider-reason"}`)
				} else {
					fmt.Fprint(w, `{"status":"pending"}`)
				}
			}))
			defer srv.Close()
			c := Client{HTTP: srv.Client()}
			_, _, _, e := c.ImageDetailed(context.Background(), Config{Endpoint: srv.URL, Timeout: 30 * time.Millisecond}, "x", nil, "1:1")
			if failure {
				if !errors.Is(e, ErrGeneration) || strings.Contains(e.Error(), "private") {
					t.Fatalf("failure %v", e)
				}
			} else if !errors.Is(e, ErrTimeout) {
				t.Fatalf("timeout %v", e)
			}
		})
	}
}
