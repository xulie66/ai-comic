package generation

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"unicode"
)

type storyChunk struct {
	text, before, after   string
	episode, index, pages int
}
type storyResult struct {
	document map[string]any
	usage    Usage
	err      error
}

func sumUsage(a, b Usage) Usage {
	return Usage{a.InputTokens + b.InputTokens, a.OutputTokens + b.OutputTokens, a.CachedTokens + b.CachedTokens}
}
func clonePayload(p map[string]any) map[string]any {
	out := make(map[string]any, len(p))
	for k, v := range p {
		out[k] = v
	}
	return out
}
func compactSource(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}
func splitStory(text string, limit int) []string {
	r := []rune(text)
	out := []string{}
	for len(r) > 0 {
		end := len(r)
		if end > limit {
			end = limit
			for i := limit - 1; i >= limit/2; i-- {
				if strings.ContainsRune("\n。！？!?；;", r[i]) {
					end = i + 1
					break
				}
			}
		}
		out = append(out, string(r[:end]))
		r = r[end:]
	}
	return out
}
func storyChunks(p map[string]any) []storyChunk {
	limit := int(tokenCount(p["storyboardChunkSize"]))
	if limit == 0 {
		limit = 6000
	}
	if limit < 1000 {
		limit = 1000
	}
	if limit > 12000 {
		limit = 12000
	}
	sources := []string{}
	episodes := list(p["episodes"])
	if len(episodes) == 0 {
		episodes = list(p["longEpisodes"])
	}
	if len(episodes) > 0 {
		for _, episode := range episodes {
			if s, ok := episode.(string); ok {
				sources = append(sources, s)
			} else {
				sources = append(sources, str(first(obj(episode), "originalText", "text", "content")))
			}
		}
	} else {
		sources = append(sources, str(first(p, "originalText", "text")))
	}
	out := []storyChunk{}
	for ei, source := range sources {
		parts := splitStory(source, limit)
		for i, text := range parts {
			if strings.TrimSpace(text) == "" {
				if len(out) > 0 {
					out[len(out)-1].text += text
				}
				continue
			}
			before, after := "", ""
			if i > 0 {
				r := []rune(parts[i-1])
				if len(r) > 300 {
					r = r[len(r)-300:]
				}
				before = string(r)
			}
			if i+1 < len(parts) {
				r := []rune(parts[i+1])
				if len(r) > 300 {
					r = r[:300]
				}
				after = string(r)
			}
			out = append(out, storyChunk{text: text, before: before, after: after, episode: ei + 1, index: len(out) + 1})
		}
	}
	target := int(tokenCount(first(p, "targetPageCount", "pageCount")))
	if target > 0 && len(out) > 0 {
		remaining := target - len(out)
		if remaining < 0 {
			remaining = 0
		}
		total := 0
		for _, c := range out {
			total += len([]rune(c.text))
		}
		type remainder struct {
			i int
			r int
		}
		rs := []remainder{}
		assigned := 0
		for i := range out {
			n := remaining * len([]rune(out[i].text))
			extra := n / total
			out[i].pages = 1 + extra
			assigned += extra
			rs = append(rs, remainder{i, n % total})
		}
		sort.SliceStable(rs, func(i, j int) bool { return rs[i].r > rs[j].r })
		for i := 0; i < remaining-assigned; i++ {
			out[rs[i].i].pages++
		}
	}
	return out
}

// ValidateStoryboard checks the generation boundary before any generated pages are persisted.
// Source coverage is literal and ordered, ignoring whitespace only; missing or invented ranges fail.
func ValidateStoryboard(document map[string]any, source string) error {
	pages, ok := document["pages"].([]any)
	if !ok || len(pages) == 0 {
		return ErrResponse
	}
	var coverage strings.Builder
	for _, v := range pages {
		page, ok := v.(map[string]any)
		if !ok {
			return ErrResponse
		}
		panels, ok := page["panels"].([]any)
		if !ok || len(panels) < 1 || len(panels) > 4 {
			return ErrResponse
		}
		segment, ok := page["original_text_range"].(string)
		if !ok || strings.TrimSpace(segment) == "" {
			return ErrResponse
		}
		coverage.WriteString(segment)
		for _, v := range panels {
			panel, ok := v.(map[string]any)
			if !ok {
				return ErrResponse
			}
			visual := first(panel, "visual_prompt", "image_prompt", "imagePrompt")
			if _, ok := visual.(string); !ok || strings.TrimSpace(str(visual)) == "" {
				return ErrResponse
			}
			if chars, exists := panel["characters"]; exists {
				if _, ok := chars.([]any); !ok {
					return ErrResponse
				}
			}
			for _, key := range []string{"title_text", "narration_text", "narration", "dialogue_text", "dialog", "dialogue", "phone_text"} {
				if value, exists := panel[key]; exists && !validTextField(value) {
					return ErrResponse
				}
			}
		}
	}
	if source != "" && compactSource(coverage.String()) != compactSource(source) {
		return ErrResponse
	}
	for _, key := range []string{"characterLibrary", "supportingCharacters"} {
		if value, exists := document[key]; exists {
			items, ok := value.([]any)
			if !ok {
				return ErrResponse
			}
			for _, item := range items {
				ch, ok := item.(map[string]any)
				if !ok {
					return ErrResponse
				}
				name, ok := ch["name"].(string)
				if !ok || strings.TrimSpace(name) == "" {
					return ErrResponse
				}
			}
		}
	}
	return nil
}
func validTextField(v any) bool {
	if v == nil {
		return true
	}
	switch x := v.(type) {
	case string:
		return true
	case []any:
		for _, item := range x {
			if !validTextField(item) {
				return false
			}
		}
		return true
	case map[string]any:
		for _, key := range []string{"text", "content", "dialogue", "dialog"} {
			if s, exists := x[key]; exists {
				_, ok := s.(string)
				return ok
			}
		}
	}
	return false
}
func charactersFrom(p map[string]any) []any {
	for _, key := range []string{"canonicalCharacters", "characterLibrary"} {
		if chars := list(p[key]); len(chars) > 0 {
			return dedupCharacters(chars)
		}
	}
	if sb := obj(p["storyboard"]); len(sb) > 0 {
		return dedupCharacters(list(sb["characterLibrary"]))
	}
	return []any{}
}
func dedupCharacters(groups ...[]any) []any {
	out := []any{}
	seen := map[string]bool{}
	for _, group := range groups {
		for _, v := range group {
			ch := obj(v)
			name := strings.ToLower(strings.Join(strings.Fields(simplified(str(ch["name"]))), ""))
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, ch)
		}
	}
	return out
}
func (c *Client) storyboardChunk(ctx context.Context, cfg Config, p map[string]any, chunk storyChunk, canonical []any) storyResult {
	request := clonePayload(p)
	request["originalText"] = chunk.text
	request["contextBefore"] = chunk.before
	request["contextAfter"] = chunk.after
	request["targetPageCount"] = chunk.pages
	if len(canonical) > 0 {
		request["canonicalCharacters"] = canonical
	}
	prompt := strings.ReplaceAll(StoryboardPrompt(request), simplified(chunk.text), chunk.text) + fmt.Sprintf("\n当前原文块编号：%d。original_text_range 必须是本块原文的连续原句，各页串联后逐字覆盖整个当前正文（可忽略空白），不得概括、重复、缺段或变更原文字形；仅可见文案转简体。", chunk.index)
	raw, usage, err := c.TextDetailed(ctx, cfg, prompt)
	result := storyResult{usage: usage, err: err}
	if err != nil {
		return result
	}
	if json.Unmarshal(raw, &result.document) != nil || ValidateStoryboard(result.document, chunk.text) != nil {
		result.err = c.fail(ctx, "storyboard schema or source coverage", ErrResponse)
	}
	return result
}

// BuildStoryboard splits long source text without dropping paragraphs, bounds provider
// concurrency to three, and merges results in source order. Each provider call contributes usage once.
func (c *Client) BuildStoryboard(ctx context.Context, cfg Config, payload map[string]any) (map[string]any, Usage, error) {
	chunks := storyChunks(payload)
	if len(chunks) == 0 {
		return nil, Usage{}, c.fail(ctx, "storyboard empty source", ErrResponse)
	}
	canonical := charactersFrom(payload)
	results := make([]storyResult, len(chunks))
	results[0] = c.storyboardChunk(ctx, cfg, payload, chunks[0], canonical)
	if results[0].err != nil {
		return nil, results[0].usage, results[0].err
	}
	canonical = dedupCharacters(canonical, list(results[0].document["characterLibrary"]))
	if len(canonical) == 0 {
		for _, key := range []string{"maleLead", "femaleLead"} {
			if ch := obj(results[0].document[key]); clean(ch["name"]) != "" {
				canonical = dedupCharacters(canonical, []any{ch})
			}
		}
		canonical = dedupCharacters(canonical, list(results[0].document["supportingCharacters"]))
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	workers := 3
	if len(chunks)-1 < workers {
		workers = len(chunks) - 1
	}
	for n := 0; n < workers; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				results[i] = c.storyboardChunk(ctx, cfg, payload, chunks[i], canonical)
			}
		}()
	}
	for i := 1; i < len(chunks); i++ {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	usage := Usage{}
	var firstErr error
	for _, r := range results {
		usage = sumUsage(usage, r.usage)
		if firstErr == nil && r.err != nil {
			firstErr = r.err
		}
	}
	if firstErr != nil {
		return nil, usage, firstErr
	}
	merged := clonePayload(results[0].document)
	pages := []any{}
	episodes := []any{}
	for i, result := range results {
		start := len(pages) + 1
		for _, item := range list(result.document["pages"]) {
			page := clonePayload(obj(item))
			page["page_number"] = len(pages) + 1
			page["page_index"] = len(pages) + 1
			page["source_chunk_index"] = i + 1
			page["episode_index"] = chunks[i].episode
			for j, v := range list(page["panels"]) {
				panel := obj(v)
				panel["panel_number"] = j + 1
				panel["panel_index"] = j + 1
			}
			page["panel_count"] = len(list(page["panels"]))
			pages = append(pages, page)
		}
		canonical = dedupCharacters(canonical, list(result.document["characterLibrary"]))
		episodes = append(episodes, map[string]any{"chunkIndex": i + 1, "episodeIndex": chunks[i].episode, "firstPage": start, "lastPage": len(pages), "originalText": chunks[i].text})
	}
	merged["pages"] = pages
	merged["characterLibrary"] = canonical
	merged["longTextMeta"] = map[string]any{"chunkCount": len(chunks), "completedChunks": len(chunks), "episodes": episodes}
	return merged, usage, nil
}

// Assets extracts named visual references and normalizes either a root array or an object
// with items/characters/scenes/props into a single items array.
func (c *Client) Assets(ctx context.Context, cfg Config, kind, text string) (map[string]any, Usage, error) {
	switch kind {
	case "characters", "character", "scenes", "scene", "props", "prop":
	default:
		return nil, Usage{}, c.fail(ctx, "asset kind", ErrResponse)
	}
	prompt := fmt.Sprintf("从以下小说原文提取%s视觉资产。只返回严格JSON对象{\"items\":[...]}。人物字段name,role,gender,age,identity,personality,appearance,outfit,props,importance,inferenceNotes；场景字段name,description,mood,time_of_day,key_elements,notes；道具字段name,description,notes。只提取有明确依据且可命名的具体资产，禁止泛称群体和完整原句作为name；不确定资料写未明确描述；合理推断记录notes或inferenceNotes。所有输出简体中文。\n【原文】\n%s", kind, text)
	raw, usage, err := c.TextDetailed(ctx, cfg, prompt)
	if err != nil {
		return nil, usage, err
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return nil, usage, c.fail(ctx, "asset JSON", ErrResponse)
	}
	items, ok := value.([]any)
	if !ok {
		root := obj(value)
		for _, key := range []string{"items", kind, "characters", "characterLibrary", "scenes", "props"} {
			if candidate, yes := root[key].([]any); yes {
				items = candidate
				ok = true
				break
			}
		}
	}
	if !ok {
		return nil, usage, c.fail(ctx, "asset schema", ErrResponse)
	}
	for _, v := range items {
		item, ok := v.(map[string]any)
		if !ok {
			return nil, usage, c.fail(ctx, "asset item schema", ErrResponse)
		}
		name, ok := item["name"].(string)
		if !ok || strings.TrimSpace(name) == "" {
			return nil, usage, c.fail(ctx, "asset item name", ErrResponse)
		}
	}
	return map[string]any{"items": dedupCharacters(items)}, usage, nil
}
