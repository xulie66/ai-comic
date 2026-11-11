// Package generation implements native OpenAI-compatible comic generation.
package generation

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type Client struct {
	HTTP   *http.Client
	Logger *slog.Logger
}
type Config struct {
	Endpoint, APIKey, Model string
	Protocol                string
	Timeout                 time.Duration
}
type Reference struct {
	Name, Type string
	Data       []byte
	MIME       string
}

// Public errors never contain provider bodies, credentials, endpoints or transport details.
var (
	ErrGeneration = errors.New("生成服务请求失败，请稍后重试")
	ErrTimeout    = errors.New("生成请求超时，请稍后重试")
	ErrCanceled   = errors.New("生成请求已取消")
	ErrResponse   = errors.New("生成服务返回的数据格式无效")
)

const maxResponse = 64 << 20

func (c *Client) fail(ctx context.Context, operation string, err error) error {
	logger := c.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.ErrorContext(ctx, "generation failed", "operation", operation, "error", err)
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return ErrTimeout
	}
	if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		return ErrCanceled
	}
	var timeout interface{ Timeout() bool }
	if errors.As(err, &timeout) && timeout.Timeout() {
		return ErrTimeout
	}
	if errors.Is(err, ErrResponse) {
		return ErrResponse
	}
	return ErrGeneration
}
func endpoint(raw, suffix string) (string, error) {
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, e := url.Parse(raw)
	if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return "", errors.New("invalid generation endpoint")
	}
	p := strings.TrimRight(u.Path, "/")
	if !strings.HasSuffix(p, "/chat/completions") && !strings.HasSuffix(p, "/images/generations") {
		p += suffix
	}
	u.Path = p
	u.Fragment = ""
	return u.String(), nil
}
func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}
func boundedRead(r io.Reader) ([]byte, error) {
	b, e := io.ReadAll(io.LimitReader(r, maxResponse+1))
	if e == nil && len(b) > maxResponse {
		return nil, errors.New("generation response exceeds limit")
	}
	return b, e
}
func (c *Client) post(ctx context.Context, cfg Config, suffix string, body any) (*http.Response, error) {
	target, e := endpoint(cfg.Endpoint, suffix)
	if e != nil {
		return nil, e
	}
	b, e := json.Marshal(body)
	if e != nil {
		return nil, e
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(b))
	if e != nil {
		return nil, e
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	resp, e := c.httpClient().Do(req)
	if e != nil {
		return nil, e
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		detail, _ := boundedRead(resp.Body)
		return nil, fmt.Errorf("provider HTTP %d: %s", resp.StatusCode, detail)
	}
	return resp, nil
}
func deadline(ctx context.Context, cfg Config) (context.Context, context.CancelFunc) {
	d := cfg.Timeout
	if d <= 0 {
		d = 15 * time.Minute
	}
	return context.WithTimeout(ctx, d)
}

func (c *Client) TextDetailed(ctx context.Context, cfg Config, prompt string) (json.RawMessage, Usage, error) {
	var usage Usage
	ctx, cancel := deadline(ctx, cfg)
	defer cancel()
	body := map[string]any{"model": cfg.Model, "messages": []any{map[string]any{"role": "user", "content": prompt}}, "stream": true, "stream_options": map[string]any{"include_usage": true}}
	resp, e := c.post(ctx, cfg, "/chat/completions", body)
	if e != nil {
		return nil, usage, c.fail(ctx, "text request", e)
	}
	defer resp.Body.Close()
	raw, e := boundedRead(resp.Body)
	if e != nil {
		return nil, usage, c.fail(ctx, "text read", e)
	}
	var content strings.Builder
	if strings.Contains(resp.Header.Get("Content-Type"), "event-stream") || bytes.HasPrefix(bytes.TrimSpace(raw), []byte("data:")) {
		s := bufio.NewScanner(bytes.NewReader(raw))
		s.Buffer(make([]byte, 4096), maxResponse)
		for s.Scan() {
			line := strings.TrimSpace(s.Text())
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			part := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if part == "[DONE]" || part == "" {
				continue
			}
			var packet map[string]any
			if e = json.Unmarshal([]byte(part), &packet); e != nil {
				return nil, usage, c.fail(ctx, "text SSE decode", ErrResponse)
			}
			if packet["error"] != nil {
				return nil, usage, c.fail(ctx, "text SSE provider", fmt.Errorf("stream error: %v", packet["error"]))
			}
			usage = mergeUsage(usage, parseUsage(packet))
			content.WriteString(completionText(packet))
		}
		if e = s.Err(); e != nil {
			return nil, usage, c.fail(ctx, "text SSE read", e)
		}
	} else {
		var packet map[string]any
		if e = json.Unmarshal(raw, &packet); e != nil {
			return nil, usage, c.fail(ctx, "text decode", ErrResponse)
		}
		usage = mergeUsage(usage, parseUsage(packet))
		if _, ok := packet["choices"]; ok {
			content.WriteString(completionText(packet))
		} else if _, ok := packet["pages"]; ok {
			content.Write(raw)
		} else {
			return nil, usage, c.fail(ctx, "text completion missing", ErrResponse)
		}
	}
	result, e := extractJSON(content.String())
	if e != nil {
		return nil, usage, c.fail(ctx, "storyboard JSON", e)
	}
	return result, usage, nil
}
func completionText(packet map[string]any) string {
	choices, _ := packet["choices"].([]any)
	if len(choices) == 0 {
		return ""
	}
	choice, _ := choices[0].(map[string]any)
	m, _ := choice["delta"].(map[string]any)
	if m == nil {
		m, _ = choice["message"].(map[string]any)
	}
	if s, ok := m["content"].(string); ok {
		return s
	}
	var b strings.Builder
	if blocks, ok := m["content"].([]any); ok {
		for _, block := range blocks {
			if v, ok := block.(map[string]any); ok {
				if s, ok := v["text"].(string); ok {
					b.WriteString(s)
				}
			}
		}
	}
	return b.String()
}
func extractJSON(text string) (json.RawMessage, error) {
	text = strings.TrimSpace(text)
	for i, r := range text {
		if r != '{' && r != '[' {
			continue
		}
		dec := json.NewDecoder(strings.NewReader(text[i:]))
		var raw json.RawMessage
		if dec.Decode(&raw) == nil {
			var v any
			if json.Unmarshal(raw, &v) == nil {
				switch v.(type) {
				case map[string]any, []any:
					return raw, nil
				}
			}
		}
		// A truncated root must not be mistaken for a complete nested object.
		return nil, ErrResponse
	}
	return nil, ErrResponse
}

func (c *Client) ImageDetailed(ctx context.Context, cfg Config, prompt string, refs []Reference, aspect string) ([]byte, string, Usage, error) {
	var usage Usage
	suffix := "/images/generations"
	switch strings.ToLower(strings.TrimSpace(cfg.Protocol)) {
	case "chat", "openai-chat", "chat-completions":
		suffix = "/chat/completions"
	}
	normalized, endpointErr := endpoint(cfg.Endpoint, suffix)
	if endpointErr != nil {
		return nil, "", usage, c.fail(ctx, "image endpoint", endpointErr)
	}
	cfg.Endpoint = normalized
	ctx, cancel := deadline(ctx, cfg)
	defer cancel()
	switch aspect {
	case "16:9", "9:16", "4:3", "3:4", "4:5", "1:1":
	default:
		aspect = "9:16"
	}
	prompt = enforceSimplified(prompt)
	urls := make([]string, 0, len(refs))
	for _, ref := range refs {
		if len(urls) == 16 {
			break
		}
		if len(ref.Data) == 0 {
			continue
		}
		mt := ref.MIME
		if mt == "" {
			mt = http.DetectContentType(ref.Data)
		}
		if !strings.HasPrefix(mt, "image/") {
			return nil, "", usage, c.fail(ctx, "image reference", errors.New("invalid reference MIME"))
		}
		urls = append(urls, "data:"+mt+";base64,"+base64.StdEncoding.EncodeToString(ref.Data))
	}
	body := map[string]any{"model": cfg.Model, "n": 1, "size": aspect, "quality": "auto", "output_format": "png"}
	if strings.Contains(strings.ToLower(cfg.Endpoint), "/chat/completions") {
		var content any = prompt
		if len(urls) > 0 {
			blocks := []any{map[string]any{"type": "text", "text": prompt}}
			for _, u := range urls {
				blocks = append(blocks, map[string]any{"type": "image_url", "image_url": map[string]any{"url": u}})
			}
			content = blocks
		}
		body["messages"] = []any{map[string]any{"role": "user", "content": content}}
		body["stream"] = false
		if strings.EqualFold(cfg.Model, "gemini-3.1-flash-image-preview") {
			body["modalities"] = []string{"text", "image"}
		}
	} else {
		body["prompt"] = prompt
		if len(urls) > 0 {
			body["image_urls"] = urls
		}
	}
	resp, e := c.post(ctx, cfg, "/images/generations", body)
	if e != nil {
		return nil, "", usage, c.fail(ctx, "image request", e)
	}
	raw, e := boundedRead(resp.Body)
	resp.Body.Close()
	if e != nil {
		return nil, "", usage, c.fail(ctx, "image read", e)
	}
	var packet any
	if json.Unmarshal(raw, &packet) != nil {
		return nil, "", usage, c.fail(ctx, "image decode", ErrResponse)
	}
	usage = parseUsage(packet)
	packet, usage, e = c.pollImage(ctx, cfg, packet, usage)
	if e != nil {
		return nil, "", usage, c.fail(ctx, "image task", e)
	}
	value := imageValue(packet)
	if value == "" {
		return nil, "", usage, c.fail(ctx, "image value", ErrResponse)
	}
	if strings.HasPrefix(value, "data:") {
		parts := strings.SplitN(value, ",", 2)
		if len(parts) != 2 || !strings.HasSuffix(parts[0], ";base64") {
			return nil, "", usage, c.fail(ctx, "image data URI", ErrResponse)
		}
		mt := strings.TrimSuffix(strings.TrimPrefix(parts[0], "data:"), ";base64")
		b, e := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(parts[1]), ""))
		if e != nil || len(b) == 0 {
			return nil, "", usage, c.fail(ctx, "image base64", ErrResponse)
		}
		if !strings.HasPrefix(mt, "image/") {
			return nil, "", usage, c.fail(ctx, "image MIME", ErrResponse)
		}
		return b, mt, usage, nil
	}
	u, e := url.Parse(value)
	if e != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, "", usage, c.fail(ctx, "image URL", ErrResponse)
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, value, nil)
	if e != nil {
		return nil, "", usage, c.fail(ctx, "image download request", e)
	}
	// Deliberately omit API Authorization when fetching a provider result URL.
	downloaded, e := c.httpClient().Do(req)
	if e != nil {
		return nil, "", usage, c.fail(ctx, "image download", e)
	}
	defer downloaded.Body.Close()
	if downloaded.StatusCode < 200 || downloaded.StatusCode >= 300 {
		return nil, "", usage, c.fail(ctx, "image download status", fmt.Errorf("HTTP %d", downloaded.StatusCode))
	}
	b, e := boundedRead(downloaded.Body)
	if e != nil {
		return nil, "", usage, c.fail(ctx, "image download read", e)
	}
	mt, _, _ := mime.ParseMediaType(downloaded.Header.Get("Content-Type"))
	if !strings.HasPrefix(mt, "image/") {
		mt = http.DetectContentType(b)
	}
	if !strings.HasPrefix(mt, "image/") || len(b) == 0 {
		return nil, "", usage, c.fail(ctx, "download MIME", ErrResponse)
	}
	return b, mt, usage, nil
}

var dataImageRE = regexp.MustCompile(`data:image/[a-zA-Z0-9.+-]+;base64,[a-zA-Z0-9+/=\r\n]+`)
var markdownImageRE = regexp.MustCompile(`!\[[^\]]*\]\((https?://[^\s)]+)\)`)
var embeddedImageRE = regexp.MustCompile(`https?://[^\s"'<>]+\.(?:png|jpe?g|webp)(?:\?[^\s"'<>]*)?`)

func imageText(s string) string {
	s = strings.TrimSpace(s)
	if v := dataImageRE.FindString(s); v != "" {
		return v
	}
	if m := markdownImageRE.FindStringSubmatch(s); len(m) > 1 {
		return m[1]
	}
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		if !strings.ContainsAny(s, " \n\r\t") {
			return s
		}
	}
	return embeddedImageRE.FindString(s)
}
func imageValue(v any) string {
	switch x := v.(type) {
	case string:
		return imageText(x)
	case []any:
		for _, item := range x {
			if s := imageValue(item); s != "" {
				return s
			}
		}
	case map[string]any:
		for _, key := range []string{"b64_json", "base64", "image_base64", "data"} {
			if s, ok := x[key].(string); ok && s != "" {
				if found := imageText(s); found != "" {
					return found
				}
				return "data:image/png;base64," + s
			}
		}
		for _, key := range []string{"url", "image_url", "imageUrl", "data", "output", "images", "results", "result", "choices", "message", "content", "text"} {
			if y, ok := x[key]; ok {
				if s := imageValue(y); s != "" {
					return s
				}
			}
		}
	}
	return ""
}
