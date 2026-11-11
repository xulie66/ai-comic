package operations

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

func (s *Service) modelInventory(ctx context.Context, channelID string) (map[string]any, error) {
	var base, key string
	if err := s.pool.QueryRow(ctx, "SELECT base_url,api_key FROM channels WHERE id=$1", channelID).Scan(&base, &key); err != nil {
		return nil, err
	}
	base = strings.TrimRight(base, "/")
	for _, suffix := range []string{"/chat/completions", "/images/generations", "/responses", "/models"} {
		base = strings.TrimSuffix(base, suffix)
	}
	if !validHTTP(base) {
		return nil, errors.New("invalid channel URL")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", base+"/models", nil)
	if err != nil {
		return nil, errors.New("invalid channel URL")
	}
	req.Header.Set("Authorization", "Bearer "+key)
	client := *s.Client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("模型列表读取失败")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, errors.New("渠道返回非成功模型列表")
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 2*1024*1024)).Decode(&payload); err != nil {
		return nil, errors.New("模型列表格式无效")
	}
	items := []map[string]any{}
	seen := map[string]bool{}
	for _, model := range payload.Data {
		id := strings.TrimSpace(model.ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		cap := "text"
		low := strings.ToLower(id)
		for _, word := range []string{"image", "imagen", "flux", "seedream", "dall-e", "sdxl", "banana"} {
			if strings.Contains(low, word) {
				cap = "image"
				break
			}
		}
		var exists bool
		if err = s.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM channel_models WHERE channel_id=$1 AND model_key=$2)", channelID, id).Scan(&exists); err != nil {
			return nil, err
		}
		items = append(items, map[string]any{"modelKey": id, "exists": exists, "capability": cap})
	}
	return map[string]any{"total": len(items), "models": items}, nil
}
