// Package operations owns PostgreSQL-backed operating, billing and model routing APIs.
package operations

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema.sql
var schema string
var ErrUnavailable = errors.New("operations database is unavailable")
var ErrInsufficient = errors.New("insufficient credits")

type identityKey struct{}
type Identity struct {
	ID      string
	IsAdmin bool
}

func WithIdentity(ctx context.Context, id string, isAdmin bool) context.Context {
	return context.WithValue(ctx, identityKey{}, Identity{id, isAdmin})
}

type Service struct {
	pool          *pgxpool.Pool
	logger        *slog.Logger
	Client        *http.Client
	DataRoot      string // Set by the host; local storage paths are confined beneath this root.
	QueueSnapshot func(context.Context) (map[string]any, error)
	RenderJobs    func(context.Context, map[string]string) (map[string]any, error)
}

func New(pool *pgxpool.Pool, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{pool: pool, logger: logger, Client: &http.Client{Timeout: 20 * time.Second}}
}
func (s *Service) Init(ctx context.Context) error {
	if s.pool == nil {
		return nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(19700218)"); err != nil {
		return err
	}
	var hadMicro bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='users' AND column_name='credits_micro')").Scan(&hadMicro); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, strings.TrimPrefix(schema, "\ufeff")); err != nil {
		return err
	}
	// Only migrate when introducing the micro column. An existing zero is authoritative.
	if !hadMicro {
		if _, err = tx.Exec(ctx, "UPDATE users SET credits_micro=(credits*1000000)::bigint"); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS operation_receipts(user_id TEXT NOT NULL, ref TEXT NOT NULL, delta_micro BIGINT NOT NULL,balance_micro BIGINT NOT NULL,created_at TIMESTAMPTZ DEFAULT now(),PRIMARY KEY(user_id,ref)); CREATE TABLE IF NOT EXISTS model_calls(id BIGSERIAL PRIMARY KEY,at TIMESTAMPTZ DEFAULT now(),user_id TEXT NOT NULL DEFAULT '',username TEXT NOT NULL DEFAULT '',capability TEXT NOT NULL DEFAULT '',scene TEXT NOT NULL DEFAULT '',provider TEXT NOT NULL DEFAULT '',endpoint TEXT NOT NULL DEFAULT '',model TEXT NOT NULL DEFAULT '',ok BOOLEAN NOT NULL DEFAULT false,status_code INTEGER NOT NULL DEFAULT 0,ms INTEGER NOT NULL DEFAULT 0,request_body TEXT DEFAULT '',response_body TEXT DEFAULT '',usage JSONB DEFAULT '{}'::jsonb,error TEXT DEFAULT '');`)
	if err != nil {
		return err
	}
	if s.DataRoot != "" {
		if _, err = tx.Exec(ctx, `INSERT INTO storage_targets(id,name,kind,root_prefix,write_enabled,read_enabled,priority,is_primary) SELECT 'stg_local_default','服务器本地磁盘','local','',true,true,10,true WHERE NOT EXISTS(SELECT 1 FROM storage_targets)`); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
func newID(prefix string) string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}

type BalanceResult struct {
	BalanceMicro int64   `json:"balanceMicro"`
	Balance      float64 `json:"balance"`
	Duplicated   bool    `json:"duplicated,omitempty"`
}

func balanceResult(n int64) BalanceResult {
	return BalanceResult{BalanceMicro: n, Balance: float64(n) / 1e6}
}
func (s *Service) Balance(ctx context.Context, id string) (BalanceResult, error) {
	if s.pool == nil {
		return BalanceResult{}, ErrUnavailable
	}
	var n int64
	err := s.pool.QueryRow(ctx, "SELECT credits_micro FROM users WHERE id=$1", id).Scan(&n)
	return balanceResult(n), err
}
func (s *Service) AdjustCredits(ctx context.Context, id string, delta int64, kind, ref, note, operator string) (BalanceResult, error) {
	if s.pool == nil {
		return BalanceResult{}, ErrUnavailable
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return BalanceResult{}, err
	}
	defer tx.Rollback(ctx)
	r, err := adjustTx(ctx, tx, id, delta, kind, ref, note, operator, false)
	if err != nil {
		return r, err
	}
	return r, tx.Commit(ctx)
}

// Charge atomically records a debit. A caller must reuse ref when retrying the same call.
func (s *Service) Charge(ctx context.Context, userID, ref string, amountMicro int64) (BalanceResult, error) {
	if amountMicro < 0 || ref == "" {
		return BalanceResult{}, errors.New("invalid charge")
	}
	if s.pool == nil {
		return BalanceResult{}, ErrUnavailable
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return BalanceResult{}, err
	}
	defer tx.Rollback(ctx)
	r, err := adjustTx(ctx, tx, userID, -amountMicro, "generation", ref, "", "generation", true)
	if err != nil {
		return r, err
	}
	return r, tx.Commit(ctx)
}
func adjustTx(ctx context.Context, tx pgx.Tx, id string, delta int64, kind, ref, note, operator string, noNegative bool) (BalanceResult, error) {
	var before int64
	var username string
	if err := tx.QueryRow(ctx, "SELECT credits_micro,username FROM users WHERE id=$1 FOR UPDATE", id).Scan(&before, &username); err != nil {
		return BalanceResult{}, err
	}
	receipt := kind + ":" + ref
	if ref != "" {
		var d, b int64
		err := tx.QueryRow(ctx, "SELECT delta_micro,balance_micro FROM operation_receipts WHERE user_id=$1 AND ref=$2", id, receipt).Scan(&d, &b)
		if err == nil {
			if d != delta {
				return BalanceResult{}, errors.New("idempotency key amount mismatch")
			}
			r := balanceResult(b)
			r.Duplicated = true
			return r, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return BalanceResult{}, err
		}
	}
	if (delta > 0 && before > math.MaxInt64-delta) || (delta < 0 && before < math.MinInt64-delta) {
		return BalanceResult{}, errors.New("credit overflow")
	}
	next := before + delta
	if noNegative && next < 0 {
		return BalanceResult{}, ErrInsufficient
	}
	if _, err := tx.Exec(ctx, "UPDATE users SET credits_micro=$2::bigint,credits=floor(($2::bigint)::numeric/1000000) WHERE id=$1", id, next); err != nil {
		return BalanceResult{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO credit_ledger(user_id,username,delta,delta_micro,balance_after,kind,ref,note,operator) VALUES($1,$2,($3::bigint)::numeric/1000000,$3::bigint,($4::bigint)::numeric,$5,$6,$7,$8)`, id, username, delta, next, kind, ref, note, operator); err != nil {
		return BalanceResult{}, err
	}
	if ref != "" {
		if _, err := tx.Exec(ctx, "INSERT INTO operation_receipts(user_id,ref,delta_micro,balance_micro) VALUES($1,$2,$3,$4)", id, receipt, delta, next); err != nil {
			return BalanceResult{}, err
		}
	}
	return balanceResult(next), nil
}

type CallLog struct {
	ID, UserID, Username, Capability, Model, ChannelID, Status, Error string
	DurationMS, InputTokens, OutputTokens, CostMicro                  int64
}

func (s *Service) RecordCall(ctx context.Context, c CallLog) error {
	if s.pool == nil {
		return ErrUnavailable
	}
	usage, _ := json.Marshal(map[string]int64{"inputTokens": c.InputTokens, "outputTokens": c.OutputTokens, "costMicro": c.CostMicro})
	_, err := s.pool.Exec(ctx, `INSERT INTO model_calls(user_id,username,capability,provider,model,ok,ms,usage,error) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, c.UserID, c.Username, c.Capability, c.ChannelID, c.Model, c.Status == "success" || c.Status == "ok", c.DurationMS, usage, redactText(c.Error))
	return err
}
func redactText(s string) string {
	if len(s) > 300 {
		return "upstream request failed"
	}
	if strings.Contains(strings.ToLower(s), "key") || strings.Contains(strings.ToLower(s), "token") || strings.Contains(s, "Bearer") {
		return "upstream request failed"
	}
	return s
}
func (s *Service) rows(ctx context.Context, q string, args ...any) ([]map[string]any, error) {
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, err
		}
		out = append(out, camelMap(m))
	}
	return out, rows.Err()
}
func camel(s string) string {
	p := strings.Split(s, "_")
	for i := 1; i < len(p); i++ {
		if p[i] != "" {
			p[i] = strings.ToUpper(p[i][:1]) + p[i][1:]
		}
	}
	return strings.Join(p, "")
}
func camelMap(m map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		out[camel(k)] = v
	}
	return out
}
func str(m map[string]any, k, def string) string {
	if v, ok := m[k]; ok && v != nil {
		return fmt.Sprint(v)
	}
	return def
}
func num(m map[string]any, k string, def int64) int64 {
	v, ok := m[k]
	if !ok {
		return def
	}
	switch x := v.(type) {
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) || x >= float64(math.MaxInt64) || x < float64(math.MinInt64) {
			return def
		}
		return int64(math.Round(x))
	case json.Number:
		n, _ := x.Int64()
		return n
	}
	return def
}
func boolean(m map[string]any, k string, def bool) bool {
	if v, ok := m[k].(bool); ok {
		return v
	}
	return def
}
func object(m map[string]any, k string) map[string]any {
	v, _ := m[k].(map[string]any)
	if v == nil {
		return map[string]any{}
	}
	return v
}
