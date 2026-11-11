package operations

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

// Channel probes only GET the API root, never generate text or images.
func (s *Service) probeChannel(ctx context.Context, id string) (map[string]any, error) {
	var base, key string
	var timeout int64
	if err := s.pool.QueryRow(ctx, "SELECT base_url,api_key,timeout_ms FROM channels WHERE id=$1", id).Scan(&base, &key, &timeout); err != nil {
		return nil, err
	}
	if !validHTTP(base) {
		return nil, errors.New("渠道地址无效")
	}
	if timeout < 1000 || timeout > 20000 {
		timeout = 20000
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", base, nil)
	if err != nil {
		return nil, errors.New("渠道地址无效")
	}
	req.Header.Set("Authorization", "Bearer "+key)
	start := time.Now()
	client := *s.Client
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	ok := false
	msg := "渠道连接失败"
	if err == nil {
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
		ok = resp.StatusCode < 500
		msg = http.StatusText(resp.StatusCode)
	}
	ms := time.Since(start).Milliseconds()
	_, dbErr := s.pool.Exec(ctx, "UPDATE channels SET last_check_at=now(),last_check_ok=$2,last_check_msg=$3,last_check_ms=$4 WHERE id=$1", id, ok, msg, ms)
	return map[string]any{"id": id, "ok": ok, "ms": ms, "message": msg, "models": []string{}}, dbErr
}
func (s *Service) extraAdmin(ctx context.Context, action string, r *http.Request, b map[string]any) (map[string]any, error, bool) {
	if action == "storage" || strings.HasPrefix(action, "storage/") {
		out, err := s.storageAdmin(ctx, action, r, b)
		return out, err, true
	}
	if out, err, handled := s.systemAdmin(ctx, action, r, b); handled {
		return out, err, true
	}
	switch action {
	case "channels/test":
		if r.Method != "POST" {
			break
		}
		ids := []string{}
		if list, ok := b["ids"].([]any); ok {
			for _, v := range list {
				if id, ok := v.(string); ok {
					ids = append(ids, id)
				}
			}
		}
		if len(ids) == 0 {
			ids = append(ids, str(b, "id", ""))
		}
		if len(ids) > 50 {
			return nil, errors.New("一次最多测试50个渠道"), true
		}
		results := []map[string]any{}
		for _, id := range ids {
			result, err := s.probeChannel(ctx, id)
			if err != nil {
				result = map[string]any{"id": id, "ok": false, "message": "渠道连接失败", "models": []string{}}
			}
			results = append(results, result)
		}
		channels, err := s.channels(ctx)
		return map[string]any{"results": results, "channels": channels}, err, true
	case "channel-models/test":
		return nil, errors.New("付费模型探测未开启，请使用渠道连通测试"), true
	case "channels/sync-models":
		if r.Method == "GET" {
			result, err := s.modelInventory(ctx, r.URL.Query().Get("id"))
			return result, err, true
		}
		if r.Method != "POST" {
			break
		}
		items, _ := b["models"].([]any)
		if len(items) == 0 || len(items) > 500 {
			return nil, errors.New("请选择1到500个模型"), true
		}
		capabilities := object(b, "capabilities")
		added := []string{}
		for _, v := range items {
			key, ok := v.(string)
			key = strings.TrimSpace(key)
			if !ok || key == "" {
				continue
			}
			cap := str(capabilities, key, "text")
			if cap != "image" {
				cap = "text"
			}
			var exists bool
			if err := s.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM channel_models WHERE channel_id=$1 AND model_key=$2 AND capability=$3)", str(b, "id", ""), key, cap).Scan(&exists); err != nil {
				return nil, err, true
			}
			if exists {
				continue
			}
			if _, err := s.saveModel(ctx, "channel_models", map[string]any{"channelId": str(b, "id", ""), "modelKey": key, "displayName": key, "capability": cap}); err != nil {
				return nil, err, true
			}
			added = append(added, key)
		}
		return map[string]any{"total": len(items), "added": added}, nil, true
	}
	return nil, nil, false
}
