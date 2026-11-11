package operations

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"golang.org/x/crypto/pbkdf2"
	"net/http"
	"runtime"
	"time"
)

func (s *Service) systemAdmin(ctx context.Context, action string, r *http.Request, b map[string]any) (map[string]any, error, bool) {
	switch action {
	case "password", "password/reset":
		if r.Method != "POST" {
			return nil, errors.New("method not allowed"), true
		}
		password := str(b, "password", "")
		if len(password) < 8 || len(password) > 4096 {
			return nil, errors.New("密码长度需8到4096字节"), true
		}
		salt := make([]byte, 16)
		if _, err := rand.Read(salt); err != nil {
			return nil, err, true
		}
		hash := pbkdf2.Key([]byte(password), salt, 120000, 32, sha256.New)
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return nil, err, true
		}
		defer tx.Rollback(ctx)
		tag, err := tx.Exec(ctx, "UPDATE users SET salt=$2,hash=$3 WHERE id=$1", str(b, "userId", ""), hex.EncodeToString(salt), hex.EncodeToString(hash))
		if err != nil {
			return nil, err, true
		}
		if tag.RowsAffected() == 0 {
			return nil, errors.New("用户不存在"), true
		}
		if _, err = tx.Exec(ctx, "DELETE FROM sessions WHERE user_id=$1", str(b, "userId", "")); err != nil {
			return nil, err, true
		}
		return nil, tx.Commit(ctx), true
	case "queue":
		if r.Method != "GET" {
			return nil, errors.New("method not allowed"), true
		}
		if s.QueueSnapshot == nil {
			return nil, errors.New("任务队列状态接口未连接"), true
		}
		out, err := s.QueueSnapshot(ctx)
		return out, err, true
	case "render-jobs":
		if r.Method != "GET" {
			return nil, errors.New("method not allowed"), true
		}
		if s.RenderJobs == nil {
			return nil, errors.New("任务列表接口未连接"), true
		}
		q := map[string]string{}
		for k := range r.URL.Query() {
			q[k] = r.URL.Query().Get(k)
		}
		out, err := s.RenderJobs(ctx, q)
		return out, err, true
	case "performance":
		if r.Method != "GET" {
			return nil, errors.New("method not allowed"), true
		}
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		var size, connections int64
		if err := s.pool.QueryRow(ctx, "SELECT pg_database_size(current_database()),(SELECT count(*) FROM pg_stat_activity WHERE datname=current_database())").Scan(&size, &connections); err != nil {
			return nil, err, true
		}
		var requests, failed, p95 int64
		if err := s.pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE status>=500),coalesce(percentile_disc(0.95) WITHIN GROUP(ORDER BY ms),0) FROM request_logs WHERE at>now()-interval '5 minutes'`).Scan(&requests, &failed, &p95); err != nil {
			return nil, err, true
		}
		now := time.Now().UTC()
		stats := map[string]any{"at": now, "heapUsed": m.HeapAlloc, "rss": nil, "goSys": m.Sys, "goRoutines": runtime.NumGoroutine(), "pgSize": size, "pgConn": connections, "reqWindow": requests, "errWindow": failed, "p95Ms": p95, "cpuPct": nil, "memTotal": nil, "diskTotal": nil, "diskUsed": nil}
		history, err := s.rows(ctx, "SELECT to_jsonb(m) FROM metrics m ORDER BY at DESC LIMIT 60")
		return map[string]any{"current": stats, "samples": history, "metrics": stats}, err, true
	case "reload":
		return nil, errors.New("原生服务配置与数据直接读取数据库，无热缓存可重载"), true
	}
	return nil, nil, false
}
