package operations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type Route struct {
	ChannelID, ChannelModelID, ChannelName, BaseURL                                               string
	APIKey                                                                                        string `json:"-"`
	ModelKey, Protocol, Provider, Capability, BillingMode                                         string
	TimeoutMS, UnitPriceMicro, InputTokenPriceMicro, OutputTokenPriceMicro, CachedTokenPriceMicro int64
}

func (s *Service) ResolveRoute(ctx context.Context, code, capability string) (Route, error) {
	r := Route{}
	if s.pool == nil {
		return r, ErrUnavailable
	}
	if capability == "" {
		capability = "image"
	}
	if code == "" {
		settings, err := s.Settings(ctx)
		if err != nil {
			return r, err
		}
		code = str(object(settings, "models"), capability+"Logical", "")
	}
	err := s.pool.QueryRow(ctx, `SELECT c.id,cm.id,c.name,c.base_url,c.api_key,CASE WHEN cm.upstream_model_key<>'' THEN cm.upstream_model_key ELSE cm.model_key END,cm.protocol,c.provider,cm.capability,c.timeout_ms,cm.billing_mode,cm.unit_price_micro,cm.input_token_price_micro,cm.output_token_price_micro,cm.cached_token_price_micro FROM logical_models lm JOIN model_routes mr ON mr.logical_model_id=lm.id JOIN channel_models cm ON cm.id=mr.channel_model_id JOIN channels c ON c.id=cm.channel_id WHERE (lm.code=$1 OR ($1='' AND lm.is_default)) AND lm.capability=$2 AND lm.enabled AND mr.enabled AND cm.enabled AND c.enabled AND cm.capability=$2 ORDER BY mr.priority,c.priority,(-ln(greatest(random(),0.000001))/greatest(mr.weight,1)) LIMIT 1`, code, capability).Scan(&r.ChannelID, &r.ChannelModelID, &r.ChannelName, &r.BaseURL, &r.APIKey, &r.ModelKey, &r.Protocol, &r.Provider, &r.Capability, &r.TimeoutMS, &r.BillingMode, &r.UnitPriceMicro, &r.InputTokenPriceMicro, &r.OutputTokenPriceMicro, &r.CachedTokenPriceMicro)
	return r, err
}
func (s *Service) channels(ctx context.Context) ([]map[string]any, error) {
	items, err := s.rows(ctx, `SELECT to_jsonb(c)-'api_key' || jsonb_build_object('hasKey',api_key<>'','apiKeyMasked',CASE WHEN api_key<>'' THEN '********' ELSE '' END) FROM channels c ORDER BY sort_order,name`)
	if err != nil {
		return nil, err
	}
	for _, c := range items {
		models, err := s.rows(ctx, `SELECT to_jsonb(cm) FROM channel_models cm WHERE channel_id=$1 ORDER BY sort_order,model_key`, c["id"])
		if err != nil {
			return nil, err
		}
		c["models"] = models
		c["apiKey"] = ""
	}
	return items, nil
}
func (s *Service) logicalModels(ctx context.Context) ([]map[string]any, error) {
	items, err := s.rows(ctx, "SELECT to_jsonb(lm) FROM logical_models lm ORDER BY sort_order,name")
	if err != nil {
		return nil, err
	}
	for _, m := range items {
		routes, err := s.rows(ctx, `SELECT to_jsonb(r) || jsonb_build_object('channelId',cm.channel_id,'channelName',c.name,'channelModelKey',cm.model_key,'available',cm.enabled AND c.enabled) FROM model_routes r JOIN channel_models cm ON cm.id=r.channel_model_id JOIN channels c ON c.id=cm.channel_id WHERE logical_model_id=$1 ORDER BY r.priority`, m["id"])
		if err != nil {
			return nil, err
		}
		m["routes"] = routes
	}
	return items, nil
}

type field struct {
	name string
	def  any
}

var channelFields = []field{{"name", "新渠道"}, {"provider", "openai-compat"}, {"base_url", ""}, {"api_key", ""}, {"enabled", true}, {"priority", int64(100)}, {"weight", int64(1)}, {"timeout_ms", int64(60000)}, {"notes", ""}, {"sort_order", int64(100)}}
var modelFields = []field{{"channel_id", ""}, {"model_key", ""}, {"display_name", ""}, {"capability", "image"}, {"enabled", true}, {"billing_mode", "unit"}, {"unit_price_micro", int64(0)}, {"input_token_price_micro", int64(0)}, {"output_token_price_micro", int64(0)}, {"cached_token_price_micro", int64(0)}, {"protocol", "openai"}, {"upstream_model_key", ""}, {"sort_order", int64(100)}, {"note", ""}}
var logicalFields = []field{{"code", ""}, {"name", ""}, {"icon", ""}, {"description", ""}, {"capability", "image"}, {"enabled", true}, {"sort_order", int64(100)}, {"price_policy", "channel"}, {"billing_mode", "per_page"}, {"unit_price", int64(0)}, {"credit_cost", int64(1)}}

func (s *Service) saveModel(ctx context.Context, table string, b map[string]any) (string, error) {
	var fs []field
	switch table {
	case "channels":
		fs = channelFields
	case "channel_models":
		fs = modelFields
	case "logical_models":
		fs = logicalFields
	default:
		return "", errors.New("invalid model type")
	}
	id := str(b, "id", "")
	if id == "" {
		id = newID("model")
	}
	if table == "logical_models" && (str(b, "code", "") == "" || str(b, "name", "") == "") {
		return "", errors.New("code and name are required")
	}
	if table == "channel_models" && (str(b, "channelId", "") == "" || str(b, "modelKey", "") == "") {
		return "", errors.New("channel and model are required")
	}
	if v := str(b, "capability", ""); v != "" && v != "image" && v != "text" {
		return "", errors.New("invalid capability")
	}
	cols := []string{"id"}
	marks := []string{"$1"}
	sets := []string{}
	args := []any{id}
	for _, f := range fs {
		k := camel(f.name)
		v, ok := b[k]
		if !ok {
			v = f.def
		}
		switch d := f.def.(type) {
		case int64:
			v = num(b, k, d)
		case bool:
			v = boolean(b, k, d)
		case string:
			v = str(b, k, d)
		}
		if f.name == "api_key" && (v == "" || strings.Contains(fmt.Sprint(v), "*")) {
			v = ""
		}
		cols = append(cols, f.name)
		args = append(args, v)
		marks = append(marks, fmt.Sprintf("$%d", len(args)))
		if f.name == "api_key" {
			sets = append(sets, "api_key=CASE WHEN EXCLUDED.api_key='' THEN channels.api_key ELSE EXCLUDED.api_key END")
		} else {
			sets = append(sets, f.name+"=EXCLUDED."+f.name)
		}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	if table == "channel_models" {
		var exists bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM channels WHERE id=$1)", str(b, "channelId", "")).Scan(&exists); err != nil {
			return "", err
		}
		if !exists {
			return "", errors.New("channel not found")
		}
	}
	_, err = tx.Exec(ctx, "INSERT INTO "+table+"("+strings.Join(cols, ",")+") VALUES("+strings.Join(marks, ",")+") ON CONFLICT(id) DO UPDATE SET "+strings.Join(sets, ","), args...)
	if err != nil {
		return "", err
	}
	if table == "logical_models" {
		if routes, ok := b["routes"].([]any); ok {
			if _, err = tx.Exec(ctx, "DELETE FROM model_routes WHERE logical_model_id=$1", id); err != nil {
				return "", err
			}
			for _, v := range routes {
				r, ok := v.(map[string]any)
				if !ok {
					continue
				}
				cm := str(r, "channelModelId", "")
				if cm == "" {
					continue
				}
				var exists bool
				if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM channel_models WHERE id=$1 AND capability=$2)", cm, str(b, "capability", "image")).Scan(&exists); err != nil {
					return "", err
				}
				if !exists {
					return "", errors.New("route model missing or capability mismatch")
				}
				if _, err = tx.Exec(ctx, "INSERT INTO model_routes(id,logical_model_id,channel_model_id,enabled,priority,weight) VALUES($1,$2,$3,$4,$5,$6)", newID("route"), id, cm, boolean(r, "enabled", true), num(r, "priority", 100), num(r, "weight", 1)); err != nil {
					return "", err
				}
			}
		}
	}
	return id, tx.Commit(ctx)
}
func (s *Service) deleteModel(ctx context.Context, table, id string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	switch table {
	case "channels":
		_, err = tx.Exec(ctx, "DELETE FROM model_routes WHERE channel_model_id IN (SELECT id FROM channel_models WHERE channel_id=$1)", id)
		if err == nil {
			_, err = tx.Exec(ctx, "DELETE FROM channel_models WHERE channel_id=$1", id)
		}
	case "channel_models":
		_, err = tx.Exec(ctx, "DELETE FROM model_routes WHERE channel_model_id=$1", id)
	case "logical_models":
		_, err = tx.Exec(ctx, "DELETE FROM model_routes WHERE logical_model_id=$1", id)
	default:
		return errors.New("invalid model type")
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "DELETE FROM "+table+" WHERE id=$1", id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *Service) setDefault(ctx context.Context, id string) (map[string]any, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(19700217)"); err != nil {
		return nil, err
	}
	var code, cap string
	if err = tx.QueryRow(ctx, "SELECT code,capability FROM logical_models WHERE id=$1 AND enabled FOR UPDATE", id).Scan(&code, &cap); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, "UPDATE logical_models SET is_default=(id=$1) WHERE capability=$2", id, cap); err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(map[string]string{cap + "Logical": code})
	if _, err = tx.Exec(ctx, `INSERT INTO settings(key,value) VALUES('models',$1) ON CONFLICT(key) DO UPDATE SET value=settings.value || EXCLUDED.value,updated_at=now()`, raw); err != nil {
		return nil, err
	}
	return map[string]any{"code": code, "capability": cap}, tx.Commit(ctx)
}
