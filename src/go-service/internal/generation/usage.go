package generation

import (
	"context"
	"encoding/json"
	"strconv"
)

// Usage contains provider-reported token counts. CachedTokens is a subset of InputTokens.
// Missing counters remain zero; no estimates are used for billing.
type Usage struct{ InputTokens, OutputTokens, CachedTokens int64 }

func (c *Client) Text(ctx context.Context, cfg Config, prompt string) (json.RawMessage, error) {
	raw, _, err := c.TextDetailed(ctx, cfg, prompt)
	return raw, err
}
func (c *Client) Image(ctx context.Context, cfg Config, prompt string, refs []Reference, aspect string) ([]byte, string, error) {
	b, m, _, err := c.ImageDetailed(ctx, cfg, prompt, refs, aspect)
	return b, m, err
}
func tokenCount(v any) int64 {
	var n int64
	switch x := v.(type) {
	case float64:
		n = int64(x)
	case int64:
		n = x
	case int:
		n = int64(x)
	case json.Number:
		n, _ = x.Int64()
	case string:
		n, _ = strconv.ParseInt(x, 10, 64)
	}
	if n < 0 {
		return 0
	}
	return n
}
func counter(m map[string]any, keys ...string) int64 {
	for _, key := range keys {
		if v, ok := m[key]; ok {
			return tokenCount(v)
		}
	}
	return 0
}
func parseUsage(packet any) Usage {
	root := obj(packet)
	m := obj(root["usage"])
	if len(m) == 0 {
		m = obj(root["usageMetadata"])
	}
	if len(m) == 0 {
		if data, ok := root["data"].(map[string]any); ok {
			return parseUsage(data)
		}
		if data := list(root["data"]); len(data) > 0 {
			return parseUsage(data[0])
		}
		return Usage{}
	}
	u := Usage{InputTokens: counter(m, "prompt_tokens", "input_tokens", "promptTokenCount"), OutputTokens: counter(m, "completion_tokens", "output_tokens", "candidatesTokenCount"), CachedTokens: counter(m, "prompt_cache_hit_tokens", "cache_read_input_tokens", "cached_tokens", "cachedContentTokenCount")}
	for _, key := range []string{"prompt_tokens_details", "input_tokens_details"} {
		if detail := obj(m[key]); len(detail) > 0 {
			if _, ok := detail["cached_tokens"]; ok {
				u.CachedTokens = tokenCount(detail["cached_tokens"])
			}
		}
	}
	// Anthropic's input_tokens excludes cache reads and cache creation. OpenAI's
	// prompt_tokens already includes cached input, so never add cache counters there.
	_, hasInput := m["input_tokens"]
	_, hasPrompt := m["prompt_tokens"]
	_, hasRead := m["cache_read_input_tokens"]
	_, hasCreation := m["cache_creation_input_tokens"]
	if hasInput && !hasPrompt && (hasRead || hasCreation) {
		u.InputTokens = tokenCount(m["input_tokens"]) + tokenCount(m["cache_read_input_tokens"]) + tokenCount(m["cache_creation_input_tokens"])
		u.CachedTokens = tokenCount(m["cache_read_input_tokens"])
	}
	return u
}

// Provider stream and poll usage reports are cumulative snapshots, not token deltas.
func mergeUsage(a, b Usage) Usage {
	if b.InputTokens > a.InputTokens {
		a.InputTokens = b.InputTokens
	}
	if b.OutputTokens > a.OutputTokens {
		a.OutputTokens = b.OutputTokens
	}
	if b.CachedTokens > a.CachedTokens {
		a.CachedTokens = b.CachedTokens
	}
	return a
}
