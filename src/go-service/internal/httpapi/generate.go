package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"comic.local/go-service/internal/generation"
	"comic.local/go-service/internal/operations"
	"comic.local/go-service/internal/store"
)

func (s *Server) settingsPath(ctx context.Context, uid, pid string) (string, error) {
	root, e := s.Store.ProjectRoot(ctx, uid, pid)
	if e != nil {
		return "", e
	}
	return filepath.Join(root, "config", "settings.json"), nil
}
func (s *Server) loadSettings(ctx context.Context, uid, pid string) (map[string]any, error) {
	b, e := s.Store.LoadProjectJSON(ctx, uid, pid, "config/settings.json")
	if errors.Is(e, store.ErrNotFound) {
		return map[string]any{}, nil
	}
	if e != nil {
		return nil, e
	}
	var p map[string]any
	e = json.Unmarshal(b, &p)
	return p, e
}
func publicSettings(p map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range p {
		if strings.Contains(strings.ToLower(k), "key") || strings.Contains(strings.ToLower(k), "url") {
			continue
		}
		out[k] = v
	}
	out["textApiKey"] = ""
	out["imageApiKey"] = ""
	return out
}
func (s *Server) saveSettings(ctx context.Context, uid, pid string, p map[string]any) error {
	u, e := s.Store.UserByID(ctx, uid)
	if e != nil {
		return e
	}
	old, e := s.loadSettings(ctx, uid, pid)
	if e != nil {
		return e
	}
	for k, v := range p {
		internal := strings.Contains(strings.ToLower(k), "key") || strings.Contains(strings.ToLower(k), "url")
		if internal && !u.IsAdmin {
			continue
		}
		old[k] = v
	}
	b, e := json.Marshal(old)
	if e != nil {
		return e
	}
	return s.Store.SaveProjectJSON(ctx, uid, pid, "config/settings.json", b)
}
func writeJSON(file string, v any) error {
	if e := os.MkdirAll(filepath.Dir(file), 0700); e != nil {
		return e
	}
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(file), ".state-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if _, e = f.Write(b); e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	return os.Rename(f.Name(), file)
}
func (s *Server) generationConfig(ctx context.Context, u store.PublicUser, pid, capability string) (generation.Config, operations.Route, error) {
	if s.Store.Pool() != nil {
		route, e := s.Ops.ResolveUserRoute(ctx, u.ID, capability)
		if e != nil {
			return generation.Config{}, route, e
		}
		return generation.Config{Endpoint: route.BaseURL, APIKey: route.APIKey, Model: route.ModelKey, Protocol: route.Protocol, Timeout: time.Duration(route.TimeoutMS) * time.Millisecond}, route, nil
	}
	// Single-user local mode uses explicitly configured environment secrets, never browser-supplied keys.
	if !u.IsAdmin {
		return generation.Config{}, operations.Route{}, store.ErrForbidden
	}
	prefix := "COMIC_" + strings.ToUpper(capability)
	c := generation.Config{Endpoint: os.Getenv(prefix + "_URL"), APIKey: os.Getenv(prefix + "_KEY"), Model: os.Getenv(prefix + "_MODEL"), Protocol: os.Getenv(prefix + "_PROTOCOL"), Timeout: 5 * time.Minute}
	if c.Endpoint == "" || c.APIKey == "" || c.Model == "" {
		return c, operations.Route{}, fmt.Errorf("local model configuration missing")
	}
	return c, operations.Route{}, nil
}
func (s *Server) generate(ctx context.Context, u store.PublicUser, pid, kind string, p map[string]any) (map[string]any, error) {
	if kind == "/api/extract/assets" && str(p, "text") == "" {
		state, e := s.readState(ctx, u.ID, pid)
		if e != nil {
			return nil, e
		}
		p["text"] = state["originalText"]
	}
	capability := "image"
	if kind == "storyboard" || kind == "rewritePrompt" || kind == "/api/extract/assets" {
		capability = "text"
	}
	c, route, e := s.generationConfig(ctx, u, pid, capability)
	if e != nil {
		return nil, e
	}
	task := str(p, "_jobTaskRef")
	if task == "" {
		task = store.NewID("task")
	}
	started := time.Now()
	// Charge before dispatch and compensate on failed dispatch. The unique task ref makes retries idempotent.
	cost := route.UnitPriceMicro
	if cost < 0 {
		return nil, store.ErrInvalid
	}
	billable := false
	if s.Store.Pool() != nil {
		policy, err := s.Ops.BillingPolicy(ctx)
		if err != nil {
			return nil, err
		}
		billable = policy.Enabled && (!u.IsAdmin || policy.ChargeAdmin)
	}
	if billable {
		receipt, err := s.Ops.ReserveGeneration(ctx, u.ID, task, cost)
		if err != nil {
			return nil, err
		}
		if receipt.Status == "failed" {
			return nil, store.ErrInvalid
		}
		if receipt.Status == "ready" {
			receipt, err = s.Ops.SettleGeneration(ctx, u.ID, task, receipt.ActualMicro, receipt.HasUsage, receipt.Result)
			if err != nil {
				return nil, err
			}
		}
		if receipt.Status == "settled" {
			var cached map[string]any
			if json.Unmarshal(receipt.Result, &cached) == nil {
				return cached, nil
			}
			return nil, store.ErrInvalid
		}
	}
	release, limitErr := s.checkGenerationLimit(ctx, u, pid, kind)
	if limitErr != nil {
		if billable {
			_, _ = s.Ops.FailGeneration(context.WithoutCancel(ctx), u.ID, task)
		}
		return nil, limitErr
	}
	completed := false
	defer func() { release(completed) }()
	var usage generation.Usage
	var result map[string]any
	if capability == "text" {
		prompt := generation.StoryboardPrompt(p)
		if kind == "rewritePrompt" {
			prompt = "将以下分镜整理为简体中文，保留全部字段与原意，输出JSON：\n" + stringJSON(p)
		}
		if kind == "/api/extract/assets" {
			prompt = "从原文提取" + str(p, "kind") + "素材，返回JSON，角色包含name/appearance/outfit，场景包含name/description，使用简体中文：\n" + str(p, "text")
		}
		var raw json.RawMessage
		if kind == "storyboard" {
			var sb map[string]any
			sb, usage, e = s.Client.BuildStoryboard(ctx, c, p)
			if e == nil {
				raw, _ = json.Marshal(sb)
			}
		} else if kind == "/api/extract/assets" {
			var assets map[string]any
			assets, usage, e = s.Client.Assets(ctx, c, str(p, "kind"), str(p, "text"))
			if e == nil {
				raw, _ = json.Marshal(assets["items"])
			}
		} else {
			raw, usage, e = s.Client.TextDetailed(ctx, c, prompt)
		}
		if e == nil {
			var data any
			e = json.Unmarshal(raw, &data)
			if e == nil {
				result = map[string]any{"success": true, "taskId": task}
				if kind == "storyboard" {
					sb := object(data)
					if nested, ok := sb["storyboard"]; ok {
						sb = object(nested)
					}
					result["storyboard"] = sb
					result["characterLibrary"] = sb["characterLibrary"]
				} else if kind == "rewritePrompt" {
					result["rewrite"] = data
				} else {
					result["items"] = data
				}
			}
		}
	} else {
		prompt := generation.PagePrompt(p)
		if kind == "character" {
			prompt = generation.CharacterPrompt(p)
		}
		if kind == "scene" {
			prompt = generation.ScenePrompt(p)
		}
		saved, se := s.readState(ctx, u.ID, pid)
		if se != nil {
			e = se
		} else {
			refs, refErr := s.generationReferences(ctx, u.ID, pid, kind, p, saved)
			if refErr != nil {
				if billable {
					_, _ = s.Ops.FailGeneration(context.WithoutCancel(ctx), u.ID, task)
				}
				return nil, refErr
			}
			var b []byte
			var mimeType string
			b, mimeType, usage, e = s.Client.ImageDetailed(ctx, c, prompt, refs, first(str(p, "aspectRatio"), "9:16"))
			if e == nil {
				ext := ".png"
				if mimeType == "image/jpeg" {
					ext = ".jpg"
				}
				if mimeType == "image/webp" {
					ext = ".webp"
				}
				var file string
				file, e = s.Store.WriteImage(ctx, u.ID, pid, "output/"+task+ext, b)
				if e == nil {
					e = s.persistImage(ctx, u.ID, pid, kind, p, file)
					if e == nil {
						result = map[string]any{"success": true, "taskId": task, "imagePath": file, "seconds": time.Since(started).Seconds()}
					}
				}
			}
		}
	}
	if billable {
		if e == nil {
			actual := cost
			hasUsage := usage.InputTokens+usage.OutputTokens > 0
			if route.BillingMode == "token" && hasUsage {
				actual = tokenCost(route, usage)
			}
			raw, _ := json.Marshal(result)
			receipt, err := s.Ops.StageGeneration(context.WithoutCancel(ctx), u.ID, task, actual, hasUsage, raw)
			if err == nil {
				receipt, err = s.Ops.SettleGeneration(context.WithoutCancel(ctx), u.ID, task, actual, hasUsage, raw)
			}
			if err != nil {
				s.Logger.Error("settlement pending", "task", task, "error", err)
				e = err
			} else {
				cost = receipt.ChargedMicro
			}
		} else {
			if _, err := s.Ops.FailGeneration(context.WithoutCancel(ctx), u.ID, task); err != nil {
				s.Logger.Error("generation compensation pending", "task", task, "error", err)
			}
		}
	}
	if s.Ops != nil {
		status := "success"
		msg := ""
		if e != nil {
			status = "error"
			msg = e.Error()
		}
		_ = s.Ops.RecordCall(context.WithoutCancel(ctx), operations.CallLog{ID: task, UserID: u.ID, Username: u.Username, Capability: capability, Model: c.Model, ChannelID: route.ChannelID, Status: status, Error: msg, DurationMS: time.Since(started).Milliseconds(), CostMicro: cost})
	}
	completed = e == nil
	return result, e
}
func stringJSON(v any) string { b, _ := json.Marshal(v); return string(b) }
func (s *Server) persistImage(ctx context.Context, uid, pid, kind string, p map[string]any, file string) error {
	return s.Store.UpdateState(ctx, uid, pid, func(saved map[string]any) error {
		if kind == "page" {
			page := object(p["page"])
			key, err := pageIndex(page)
			if err != nil {
				return err
			}
			found := false
			for _, v := range array(object(saved["storyboard"])["pages"]) {
				target := object(v)
				targetKey, err := pageIndex(target)
				if err == nil && targetKey == key {
					target["page_image_url"] = file
					target["imagePath"] = file
					target["pageImageUrl"] = file
					target["imageUpdatedAt"] = time.Now().UnixMilli()
					found = true
				}
			}
			if !found {
				return store.ErrNotFound
			}
		} else {
			library, field := "characterLibrary", "referenceImagePath"
			if kind == "scene" {
				library, field = "sceneLibrary", "refImagePath"
			}
			name := str(object(p[kind]), "name")
			found := false
			var syncRoot func(map[string]any)
			syncRoot = func(root map[string]any) {
				for _, v := range array(root[library]) {
					card := object(v)
					if str(card, "name") == name {
						card[field] = file
						card["status"] = "done"
						card["imageUpdatedAt"] = time.Now().UnixMilli()
						found = true
					}
				}
			}
			syncRoot(saved)
			syncRoot(object(saved["storyboard"]))
			for _, ev := range array(saved["episodes"]) {
				syncRoot(object(object(ev)["storyboard"]))
			}
			if !found {
				return store.ErrNotFound
			}
		}
		return nil
	})
}
