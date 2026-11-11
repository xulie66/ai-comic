package operations

import (
	"context"
	"encoding/json"
	"strings"
)

const defaults = `{"site":{"name":"AI 漫画工坊","registerOpen":false},"credits":{"enabled":false,"chargeAdmin":true},"limits":{"dailyPages":80,"pagesPerMinute":4},"models":{"imageLogical":"manga-image-default","textLogical":"manga-text-default"},"registration":{"open":false,"inviteRequired":true},"redemption":{"enabled":false},"payment":{"enabled":true,"provider":"epay","creditsPerYuan":10,"epay":{"gateway":"","pid":"","key":"","payType":"alipay","payTypes":["alipay","wxpay"],"checkoutMode":"redirect","device":"pc","publicBaseUrl":""},"packages":[{"id":"p10","title":"体验包","amountYuan":10,"bonusPercent":0},{"id":"p50","title":"常用包","amountYuan":50,"bonusPercent":5},{"id":"p100","title":"创作者包","amountYuan":100,"bonusPercent":10}]}}`

func merge(a, b map[string]any) map[string]any {
	for k, v := range b {
		if vm, ok := v.(map[string]any); ok {
			if am, ok := a[k].(map[string]any); ok {
				a[k] = merge(am, vm)
				continue
			}
		}
		a[k] = v
	}
	return a
}
func (s *Service) Settings(ctx context.Context) (map[string]any, error) {
	if s.pool == nil {
		return nil, ErrUnavailable
	}
	m := map[string]any{}
	_ = json.Unmarshal([]byte(defaults), &m)
	rows, err := s.pool.Query(ctx, "SELECT key,value FROM settings")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		var raw []byte
		if err := rows.Scan(&k, &raw); err != nil {
			return nil, err
		}
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, err
		}
		merge(m, map[string]any{k: v})
	}
	return m, rows.Err()
}
func (s *Service) patchSettings(ctx context.Context, patch map[string]any) (map[string]any, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	for _, k := range []string{"site", "credits", "limits", "models", "registration", "email", "billing", "redemption", "payment"} {
		v, ok := patch[k].(map[string]any)
		if !ok {
			continue
		}
		raw, _ := json.Marshal(v)
		_, err = tx.Exec(ctx, `INSERT INTO settings(key,value) VALUES($1,$2) ON CONFLICT(key) DO UPDATE SET value=settings.value || EXCLUDED.value,updated_at=now()`, k, raw)
		if err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.Settings(ctx)
}

// publicSettings recursively removes credentials, including nested payment/email settings.
func publicSettings(v any) any {
	switch x := v.(type) {
	case map[string]any:
		m := map[string]any{}
		for k, v := range x {
			low := strings.ToLower(k)
			if low == "key" || strings.Contains(low, "password") || strings.Contains(low, "secret") || strings.Contains(low, "token") || low == "apikey" || low == "api_key" || low == "invitecode" {
				m[k] = ""
				m["has"+strings.ToUpper(k[:1])+k[1:]] = v != "" && v != nil
				continue
			}
			m[k] = publicSettings(v)
		}
		return m
	case []any:
		a := make([]any, len(x))
		for i, v := range x {
			a[i] = publicSettings(v)
		}
		return a
	}
	return v
}
func (s *Service) Packages(ctx context.Context) ([]map[string]any, error) {
	settings, err := s.Settings(ctx)
	if err != nil {
		return nil, err
	}
	p := object(settings, "payment")
	items, _ := p["packages"].([]any)
	out := []map[string]any{}
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		amount := num(m, "amountYuan", 0)
		fen := int64(0)
		if v, ok := m["amountYuan"].(float64); ok {
			fen = int64(v*100 + 0.5)
		}
		if fen <= 0 {
			continue
		}
		credits := int64(float64(fen) * float64(num(p, "creditsPerYuan", 10)) / 100 * (1 + float64(num(m, "bonusPercent", 0))/100))
		out = append(out, map[string]any{"id": str(m, "id", ""), "title": str(m, "title", ""), "amountYuan": float64(fen) / 100, "amountFen": fen, "credits": credits, "creditsMicro": credits * 1000000, "bonusPercent": num(m, "bonusPercent", 0), "note": str(m, "note", "")})
		_ = amount
	}
	return out, nil
}
