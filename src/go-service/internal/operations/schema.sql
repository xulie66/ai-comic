
ALTER TABLE users ADD COLUMN IF NOT EXISTS credits NUMERIC NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN IF NOT EXISTS credit_status TEXT NOT NULL DEFAULT 'active';
ALTER TABLE users ADD COLUMN IF NOT EXISTS banned_reason TEXT DEFAULT '';

CREATE TABLE IF NOT EXISTS settings (
  key TEXT PRIMARY KEY,
  value JSONB NOT NULL DEFAULT '{}'::jsonb,
  updated_at TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE IF NOT EXISTS request_logs (
  id BIGSERIAL PRIMARY KEY,
  at TIMESTAMPTZ NOT NULL DEFAULT now(),
  user_id TEXT DEFAULT '',
  username TEXT DEFAULT '',
  method TEXT DEFAULT '',
  path TEXT DEFAULT '',
  status INTEGER NOT NULL DEFAULT 0,
  ms INTEGER NOT NULL DEFAULT 0,
  ip TEXT DEFAULT '',
  ua TEXT DEFAULT '',
  kind TEXT DEFAULT 'api',
  note TEXT DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_req_at ON request_logs(at DESC);
CREATE INDEX IF NOT EXISTS idx_req_user ON request_logs(user_id, at DESC);
CREATE INDEX IF NOT EXISTS idx_req_status ON request_logs(status);

CREATE TABLE IF NOT EXISTS login_logs (
  id BIGSERIAL PRIMARY KEY,
  at TIMESTAMPTZ NOT NULL DEFAULT now(),
  user_id TEXT DEFAULT '',
  username TEXT DEFAULT '',
  ip TEXT DEFAULT '',
  ua TEXT DEFAULT '',
  ok BOOLEAN NOT NULL DEFAULT true,
  reason TEXT DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_login_at ON login_logs(at DESC);
CREATE INDEX IF NOT EXISTS idx_login_user ON login_logs(username, at DESC);

CREATE TABLE IF NOT EXISTS metrics (
  id BIGSERIAL PRIMARY KEY,
  at TIMESTAMPTZ NOT NULL DEFAULT now(),
  cpu_pct REAL, load1 REAL, mem_used BIGINT, mem_total BIGINT,
  disk_used BIGINT, disk_total BIGINT, rss BIGINT, heap_used BIGINT,
  pg_size BIGINT, pg_conn INTEGER, req_window INTEGER, err_window INTEGER, p95_ms INTEGER
);
CREATE INDEX IF NOT EXISTS idx_metrics_at ON metrics(at DESC);

CREATE TABLE IF NOT EXISTS credit_ledger (
  id BIGSERIAL PRIMARY KEY,
  at TIMESTAMPTZ NOT NULL DEFAULT now(),
  user_id TEXT NOT NULL,
  username TEXT DEFAULT '',
  delta NUMERIC NOT NULL DEFAULT 0,
  balance_after NUMERIC NOT NULL DEFAULT 0,
  kind TEXT NOT NULL DEFAULT 'adjust',
  ref TEXT DEFAULT '',
  note TEXT DEFAULT '',
  operator TEXT DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_ledger_at ON credit_ledger(at DESC);
CREATE INDEX IF NOT EXISTS idx_ledger_user ON credit_ledger(user_id, at DESC);

CREATE TABLE IF NOT EXISTS payments (
  id TEXT PRIMARY KEY,
  at TIMESTAMPTZ NOT NULL DEFAULT now(),
  user_id TEXT DEFAULT '',
  username TEXT DEFAULT '',
  amount NUMERIC NOT NULL DEFAULT 0,
  credits NUMERIC NOT NULL DEFAULT 0,
  method TEXT DEFAULT 'manual',
  status TEXT NOT NULL DEFAULT 'pending',
  trade_no TEXT DEFAULT '',
  note TEXT DEFAULT '',
  paid_at TIMESTAMPTZ,
  operator TEXT DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_pay_at ON payments(at DESC);
CREATE INDEX IF NOT EXISTS idx_pay_status ON payments(status);

CREATE TABLE IF NOT EXISTS redeem_codes (
  id TEXT PRIMARY KEY,
  code TEXT UNIQUE NOT NULL,
  credits NUMERIC NOT NULL DEFAULT 0,
  batch TEXT DEFAULT '',
  status TEXT NOT NULL DEFAULT 'unused',
  used_by TEXT DEFAULT '',
  used_at TIMESTAMPTZ,
  expires_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ DEFAULT now(),
  note TEXT DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_code_batch ON redeem_codes(batch);
CREATE INDEX IF NOT EXISTS idx_code_status ON redeem_codes(status);

CREATE TABLE IF NOT EXISTS storage_targets (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  kind TEXT NOT NULL DEFAULT 'local',
  endpoint TEXT DEFAULT '',
  bucket TEXT DEFAULT '',
  region TEXT DEFAULT '',
  access_key TEXT DEFAULT '',
  secret_key TEXT DEFAULT '',
  public_base TEXT DEFAULT '',
  root_prefix TEXT DEFAULT '',
  write_enabled BOOLEAN NOT NULL DEFAULT false,
  read_enabled BOOLEAN NOT NULL DEFAULT true,
  priority INTEGER NOT NULL DEFAULT 100,
  is_primary BOOLEAN NOT NULL DEFAULT false,
  created_at TIMESTAMPTZ DEFAULT now(),
  updated_at TIMESTAMPTZ,
  last_check_at TIMESTAMPTZ,
  last_check_ok BOOLEAN,
  last_check_msg TEXT DEFAULT ''
);

CREATE TABLE IF NOT EXISTS storage_writes (
  id BIGSERIAL PRIMARY KEY,
  at TIMESTAMPTZ NOT NULL DEFAULT now(),
  target_id TEXT DEFAULT '',
  object_key TEXT DEFAULT '',
  bytes BIGINT DEFAULT 0,
  ok BOOLEAN NOT NULL DEFAULT false,
  ms INTEGER DEFAULT 0,
  error TEXT DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_sw_at ON storage_writes(at DESC);

CREATE TABLE IF NOT EXISTS channels (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  provider TEXT NOT NULL DEFAULT 'openai',
  base_url TEXT DEFAULT '',
  api_key TEXT DEFAULT '',
  enabled BOOLEAN NOT NULL DEFAULT true,
  priority INTEGER NOT NULL DEFAULT 100,
  weight INTEGER NOT NULL DEFAULT 1,
  timeout_ms INTEGER NOT NULL DEFAULT 60000,
  notes TEXT DEFAULT '',
  sort_order INTEGER NOT NULL DEFAULT 100,
  created_at TIMESTAMPTZ DEFAULT now(),
  updated_at TIMESTAMPTZ,
  last_check_at TIMESTAMPTZ,
  last_check_ok BOOLEAN,
  last_check_msg TEXT DEFAULT '',
  last_check_ms INTEGER
);

CREATE TABLE IF NOT EXISTS channel_models (
  id TEXT PRIMARY KEY,
  channel_id TEXT NOT NULL,
  model_key TEXT NOT NULL,
  display_name TEXT DEFAULT '',
  capability TEXT NOT NULL DEFAULT 'image',
  enabled BOOLEAN NOT NULL DEFAULT true,
  unit_cost NUMERIC NOT NULL DEFAULT 0,
  note TEXT DEFAULT '',
  last_check_at TIMESTAMPTZ,
  last_check_ok BOOLEAN,
  last_check_msg TEXT DEFAULT '',
  created_at TIMESTAMPTZ DEFAULT now(),
  UNIQUE (channel_id, model_key, capability)
);
CREATE INDEX IF NOT EXISTS idx_cm_channel ON channel_models(channel_id);

CREATE TABLE IF NOT EXISTS logical_models (
  id TEXT PRIMARY KEY,
  code TEXT UNIQUE NOT NULL,
  name TEXT NOT NULL,
  icon TEXT DEFAULT '',
  description TEXT DEFAULT '',
  capability TEXT NOT NULL DEFAULT 'image',
  enabled BOOLEAN NOT NULL DEFAULT true,
  sort_order INTEGER NOT NULL DEFAULT 100,
  price_policy TEXT NOT NULL DEFAULT 'channel',
  billing_mode TEXT NOT NULL DEFAULT 'per_page',
  unit_price NUMERIC NOT NULL DEFAULT 0,
  credit_cost NUMERIC NOT NULL DEFAULT 1,
  created_at TIMESTAMPTZ DEFAULT now(),
  updated_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS model_routes (
  id TEXT PRIMARY KEY,
  logical_model_id TEXT NOT NULL,
  channel_model_id TEXT NOT NULL,
  enabled BOOLEAN NOT NULL DEFAULT true,
  priority INTEGER NOT NULL DEFAULT 100,
  weight INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX IF NOT EXISTS idx_route_model ON model_routes(logical_model_id);

-- Per-call and per-token prices are denominated in micro credits.
ALTER TABLE channel_models ADD COLUMN IF NOT EXISTS billing_mode TEXT NOT NULL DEFAULT 'unit';
ALTER TABLE channel_models ADD COLUMN IF NOT EXISTS unit_price_micro BIGINT NOT NULL DEFAULT 0;
ALTER TABLE channel_models ADD COLUMN IF NOT EXISTS input_token_price_micro BIGINT NOT NULL DEFAULT 0;
ALTER TABLE channel_models ADD COLUMN IF NOT EXISTS output_token_price_micro BIGINT NOT NULL DEFAULT 0;
ALTER TABLE channel_models ADD COLUMN IF NOT EXISTS cached_token_price_micro BIGINT NOT NULL DEFAULT 0;
ALTER TABLE channel_models ADD COLUMN IF NOT EXISTS protocol TEXT NOT NULL DEFAULT 'openai';
ALTER TABLE channel_models ADD COLUMN IF NOT EXISTS upstream_model_key TEXT DEFAULT '';
ALTER TABLE channel_models ADD COLUMN IF NOT EXISTS sort_order INTEGER NOT NULL DEFAULT 100;

-- Micro credits are authoritative after the initial migration.
ALTER TABLE users ADD COLUMN IF NOT EXISTS credits_micro BIGINT NOT NULL DEFAULT 0;
ALTER TABLE credit_ledger ADD COLUMN IF NOT EXISTS delta_micro BIGINT NOT NULL DEFAULT 0;

-- Logical default flags.
ALTER TABLE logical_models ADD COLUMN IF NOT EXISTS is_default BOOLEAN NOT NULL DEFAULT false;

-- Payment provider metadata and upstream transaction uniqueness.
ALTER TABLE payments ADD COLUMN IF NOT EXISTS provider TEXT DEFAULT 'manual';
ALTER TABLE payments ADD COLUMN IF NOT EXISTS pay_type TEXT DEFAULT '';
ALTER TABLE payments ADD COLUMN IF NOT EXISTS credits_micro BIGINT NOT NULL DEFAULT 0;
ALTER TABLE payments ADD COLUMN IF NOT EXISTS checkout_mode TEXT DEFAULT '';
ALTER TABLE payments ADD COLUMN IF NOT EXISTS checkout_url TEXT DEFAULT '';
ALTER TABLE payments ADD COLUMN IF NOT EXISTS provider_trade_no TEXT DEFAULT '';
ALTER TABLE payments ADD COLUMN IF NOT EXISTS notify_json TEXT DEFAULT '';
ALTER TABLE payments ADD COLUMN IF NOT EXISTS paid_at TIMESTAMPTZ;
CREATE UNIQUE INDEX IF NOT EXISTS idx_pay_provider_trade ON payments(provider, provider_trade_no) WHERE provider_trade_no <> '';

-- Preserve compatibility with legacy settings tables.
ALTER TABLE settings ADD COLUMN IF NOT EXISTS key TEXT;

-- Durable generation state and result; retries must reuse the same task reference.
CREATE TABLE IF NOT EXISTS generation_receipts (
 user_id TEXT NOT NULL,
 task_ref TEXT NOT NULL,
 status TEXT NOT NULL DEFAULT 'reserved',
 reserved_micro BIGINT NOT NULL DEFAULT 0,
 charged_micro BIGINT NOT NULL DEFAULT 0,
 balance_micro BIGINT NOT NULL DEFAULT 0,
 result JSONB NOT NULL DEFAULT 'null'::jsonb,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY(user_id,task_ref),
 CONSTRAINT generation_receipts_status_check CHECK(status IN ('reserved','ready','settled','failed')),
 CHECK(reserved_micro>=0 AND charged_micro>=0)
);
ALTER TABLE generation_receipts ADD COLUMN IF NOT EXISTS actual_micro BIGINT NOT NULL DEFAULT 0;
ALTER TABLE generation_receipts ADD COLUMN IF NOT EXISTS has_usage BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE generation_receipts DROP CONSTRAINT IF EXISTS generation_receipts_status_check;
ALTER TABLE generation_receipts ADD CONSTRAINT generation_receipts_status_check CHECK(status IN ('reserved','ready','settled','failed'));

