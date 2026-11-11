package generation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func imageTask(packet any) (id, status, statusURL string) {
	root := obj(packet)
	data := obj(root["data"])
	if arr := list(root["data"]); len(arr) > 0 {
		data = obj(arr[0])
	}
	id = str(first(data, "task_id", "taskId"))
	if id == "" {
		id = str(first(root, "task_id", "taskId"))
	}
	status = str(first(data, "status"))
	if status == "" {
		status = str(root["status"])
	}
	statusURL = str(first(data, "statusUrl", "status_url"))
	if statusURL == "" {
		statusURL = str(first(root, "statusUrl", "status_url"))
	}
	return
}
func failedTask(s string) bool {
	switch strings.ToLower(s) {
	case "failed", "failure", "error", "cancelled", "canceled", "rejected":
		return true
	}
	return false
}
func doneTask(s string) bool {
	switch strings.ToLower(s) {
	case "completed", "complete", "success", "succeeded", "done":
		return true
	}
	return false
}
func (c *Client) pollImage(ctx context.Context, cfg Config, packet any, usage Usage) (any, Usage, error) {
	id, status, statusURL := imageTask(packet)
	if failedTask(status) {
		return packet, usage, fmt.Errorf("provider task failed: %v", packet)
	}
	if imageValue(packet) != "" || id == "" {
		return packet, usage, nil
	}
	baseRaw, err := endpoint(cfg.Endpoint, "/images/generations")
	if err != nil {
		return packet, usage, err
	}
	base, _ := url.Parse(baseRaw)
	target := *base
	if statusURL != "" {
		relative, err := url.Parse(statusURL)
		if err != nil {
			return packet, usage, errors.New("invalid task status URL")
		}
		target = *base.ResolveReference(relative)
	} else {
		target.Path = "/v1/images/tasks/" + url.PathEscape(id)
		target.RawPath = ""
		target.RawQuery = ""
	}
	if (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" || target.User != nil {
		return packet, usage, errors.New("invalid task status URL")
	}
	// Only follow the one explicitly advertised status URL or the documented default.
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
		if err != nil {
			return packet, usage, err
		}
		req.Header.Set("Accept", "application/json")
		if target.Scheme == base.Scheme && strings.EqualFold(target.Host, base.Host) && cfg.APIKey != "" {
			req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
		}
		resp, err := c.httpClient().Do(req)
		if err != nil {
			return packet, usage, err
		}
		raw, readErr := boundedRead(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return packet, usage, readErr
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return packet, usage, fmt.Errorf("task HTTP %d: %s", resp.StatusCode, raw)
		}
		if json.Unmarshal(raw, &packet) != nil {
			return packet, usage, ErrResponse
		}
		usage = mergeUsage(usage, parseUsage(packet))
		_, status, _ = imageTask(packet)
		if failedTask(status) {
			return packet, usage, fmt.Errorf("provider task failed: %v", packet)
		}
		if imageValue(packet) != "" {
			return packet, usage, nil
		}
		if doneTask(status) {
			return packet, usage, ErrResponse
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return packet, usage, ctx.Err()
		case <-timer.C:
		}
	}
}
