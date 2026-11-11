package operations

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (s *Service) storageTargets(ctx context.Context) ([]map[string]any, error) {
	return s.rows(ctx, `SELECT to_jsonb(t)-'access_key'-'secret_key' || jsonb_build_object('accessKey','','hasAccessKey',access_key<>'','hasSecret',secret_key<>'','recent',(SELECT jsonb_build_object('writes',count(*),'bytes',coalesce(sum(bytes),0),'failures',count(*) FILTER(WHERE NOT ok)) FROM storage_writes w WHERE w.target_id=t.id AND w.at>now()-interval '7 days')) FROM storage_targets t ORDER BY priority,name`)
}
func stringIDs(b map[string]any) []string {
	out := []string{}
	if list, ok := b["ids"].([]any); ok {
		for _, v := range list {
			if id, ok := v.(string); ok && id != "" {
				out = append(out, id)
			}
		}
	}
	if len(out) == 0 && str(b, "id", "") != "" {
		out = append(out, str(b, "id", ""))
	}
	return out
}
func (s *Service) saveStorage(ctx context.Context, b map[string]any) (string, error) {
	id := str(b, "id", newID("stg"))
	kind := str(b, "kind", "local")
	switch kind {
	case "local", "s3", "oss", "cos", "r2", "kodo", "webdav":
	default:
		return "", errors.New("unsupported storage kind")
	}
	if kind != "local" && !validHTTP(str(b, "endpoint", "")) {
		return "", errors.New("invalid storage endpoint")
	}
	if _, err := objectKey(str(b, "rootPrefix", ""), "probe.txt"); err != nil {
		return "", err
	}
	secret := str(b, "secretKey", "")
	access := str(b, "accessKey", "")
	if strings.Contains(secret, "*") {
		secret = ""
	}
	if strings.Contains(access, "*") {
		access = ""
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO storage_targets(id,name,kind,endpoint,bucket,region,access_key,secret_key,public_base,root_prefix,write_enabled,read_enabled,priority) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) ON CONFLICT(id) DO UPDATE SET name=EXCLUDED.name,kind=EXCLUDED.kind,endpoint=EXCLUDED.endpoint,bucket=EXCLUDED.bucket,region=EXCLUDED.region,access_key=CASE WHEN EXCLUDED.access_key='' THEN storage_targets.access_key ELSE EXCLUDED.access_key END,secret_key=CASE WHEN EXCLUDED.secret_key='' THEN storage_targets.secret_key ELSE EXCLUDED.secret_key END,public_base=EXCLUDED.public_base,root_prefix=EXCLUDED.root_prefix,write_enabled=EXCLUDED.write_enabled,read_enabled=EXCLUDED.read_enabled,priority=EXCLUDED.priority,updated_at=now()`, id, str(b, "name", "未命名存储"), kind, str(b, "endpoint", ""), str(b, "bucket", ""), str(b, "region", "us-east-1"), access, secret, str(b, "publicBase", ""), str(b, "rootPrefix", ""), boolean(b, "writeEnabled", false), boolean(b, "readEnabled", true), num(b, "priority", 100))
	return id, err
}
func (s *Service) storageAdmin(ctx context.Context, action string, r *http.Request, b map[string]any) (map[string]any, error) {
	switch action {
	case "storage":
		if r.Method != "GET" {
			return nil, errors.New("method not allowed")
		}
		targets, err := s.storageTargets(ctx)
		if err != nil {
			return nil, err
		}
		writes, err := s.rows(ctx, "SELECT to_jsonb(w) FROM storage_writes w ORDER BY at DESC LIMIT 30")
		if err != nil {
			return nil, err
		}
		stats, err := s.rows(ctx, `SELECT jsonb_build_object('total',count(*),'bytes',coalesce(sum(bytes),0),'failures',count(*) FILTER(WHERE NOT ok),'avgMs',coalesce(round(avg(ms)),0)) FROM storage_writes WHERE at>now()-interval '7 days'`)
		if err != nil {
			return nil, err
		}
		kinds, err := s.rows(ctx, "SELECT jsonb_build_object('kind',kind,'count',count(*)) FROM storage_targets GROUP BY kind")
		return map[string]any{"targets": targets, "writes": writes, "writeStats": stats[0], "kinds": kinds}, err
	case "storage/writes":
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit < 1 || limit > 200 {
			limit = 50
		}
		items, err := s.rows(ctx, "SELECT to_jsonb(w) FROM storage_writes w ORDER BY at DESC LIMIT $1", limit)
		return map[string]any{"writes": items}, err
	case "storage/save":
		if r.Method != "POST" {
			return nil, errors.New("method not allowed")
		}
		id, err := s.saveStorage(ctx, b)
		if err != nil {
			return nil, err
		}
		targets, err := s.storageTargets(ctx)
		return map[string]any{"id": id, "targets": targets}, err
	case "storage/delete":
		if r.Method != "POST" {
			return nil, errors.New("method not allowed")
		}
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return nil, err
		}
		defer tx.Rollback(ctx)
		if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(19700219)"); err != nil {
			return nil, err
		}
		var primary bool
		var count int
		if err = tx.QueryRow(ctx, "SELECT is_primary FROM storage_targets WHERE id=$1 FOR UPDATE", str(b, "id", "")).Scan(&primary); err != nil {
			return nil, err
		}
		if err = tx.QueryRow(ctx, "SELECT count(*) FROM storage_targets").Scan(&count); err != nil {
			return nil, err
		}
		if primary || count <= 1 {
			return nil, errors.New("请先切换主存储并至少保留一个目标")
		}
		if _, err = tx.Exec(ctx, "DELETE FROM storage_targets WHERE id=$1", str(b, "id", "")); err != nil {
			return nil, err
		}
		if err = tx.Commit(ctx); err != nil {
			return nil, err
		}
		targets, err := s.storageTargets(ctx)
		return map[string]any{"targets": targets}, err
	case "storage/write-targets":
		if r.Method != "POST" {
			return nil, errors.New("method not allowed")
		}
		ids := stringIDs(b)
		if len(ids) == 0 {
			return nil, errors.New("请选择存储目标")
		}
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return nil, err
		}
		defer tx.Rollback(ctx)
		if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(19700219)"); err != nil {
			return nil, err
		}
		var n int
		if err = tx.QueryRow(ctx, "SELECT count(*) FROM storage_targets WHERE id=ANY($1)", ids).Scan(&n); err != nil {
			return nil, err
		}
		if n != len(ids) {
			return nil, errors.New("存储目标不存在或重复")
		}
		if _, err = tx.Exec(ctx, "UPDATE storage_targets SET write_enabled=$2,updated_at=now() WHERE id=ANY($1)", ids, boolean(b, "writeEnabled", true)); err != nil {
			return nil, err
		}
		if boolean(b, "makePrimary", false) {
			if _, err = tx.Exec(ctx, "UPDATE storage_targets SET is_primary=(id=$1)", ids[0]); err != nil {
				return nil, err
			}
		}
		if err = tx.Commit(ctx); err != nil {
			return nil, err
		}
		targets, err := s.storageTargets(ctx)
		return map[string]any{"updated": n, "targets": targets}, err
	case "storage/test", "storage/write-check", "storage/write":
		if r.Method != "POST" {
			return nil, errors.New("method not allowed")
		}
		ids := stringIDs(b)
		probe := action == "storage/test"
		key := str(b, "key", "")
		var data []byte
		contentType := str(b, "contentType", "application/octet-stream")
		if action != "storage/write" {
			key = "probe/" + newID("check") + ".txt"
			data = []byte("comic storage probe " + time.Now().UTC().Format(time.RFC3339Nano))
			contentType = "text/plain"
		} else if text, ok := b["text"].(string); ok {
			data = []byte(text)
		} else if enc, ok := b["contentBase64"].(string); ok {
			var err error
			data, err = base64.StdEncoding.DecodeString(enc)
			if err != nil {
				return nil, errors.New("invalid base64 content")
			}
		} else {
			return nil, errors.New("缺少写入内容")
		}
		if len(data) > 32*1024*1024 {
			return nil, errors.New("单对象上限32MB")
		}
		if _, err := objectKey("", key); err != nil {
			return nil, err
		}
		if len(ids) == 0 {
			if probe {
				return nil, errors.New("请选择测试目标")
			}
			rows, err := s.rows(ctx, "SELECT jsonb_build_object('id',id) FROM storage_targets WHERE write_enabled ORDER BY priority")
			if err != nil {
				return nil, err
			}
			for _, row := range rows {
				ids = append(ids, row["id"].(string))
			}
		}
		if len(ids) == 0 || len(ids) > 100 {
			return nil, errors.New("没有可写目标或目标过多")
		}
		results := []map[string]any{}
		okCount := 0
		for _, id := range ids {
			target, err := s.target(ctx, id)
			if err != nil {
				return nil, err
			}
			if !probe && !target.WriteEnabled {
				return nil, errors.New("所选目标未开启写入")
			}
			out, err := s.writeTarget(ctx, target, key, data, contentType, probe)
			if err != nil {
				return nil, err
			}
			if out["ok"] == true {
				okCount++
			}
			results = append(results, out)
		}
		targets, err := s.storageTargets(ctx)
		return map[string]any{"key": key, "bytes": len(data), "contentType": contentType, "results": results, "okCount": okCount, "targets": targets}, err
	}
	return nil, errors.New("unknown storage endpoint")
}
