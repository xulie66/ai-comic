# Native Go operations

Integration:

```go
svc := operations.New(pool, logger)
if err := svc.Init(ctx); err != nil { return err }
r = r.WithContext(operations.WithIdentity(r.Context(), user.ID, user.IsAdmin))
svc.ServeHTTP(w, r)
```

Initialize the shared users table before `Init`. Initialization keeps the original operating schema and compatibility columns, without dropping/replacing tables. Only the first introduction of `users.credits_micro` imports legacy credits; existing micro balances, including zero, remain authoritative. `operation_receipts` is an additional Go-owned idempotency table.

`Charge(ctx,userID,ref,amountMicro)` requires a nonempty stable call reference. It locks the user, validates balance and writes the ledger/receipt in one transaction. Retry with the same reference and amount returns the original balance and `Duplicated=true`; an amount mismatch errors. For generation use the durable lifecycle instead: `ReserveGeneration` pre-debits the unit fallback and requires positive sufficient balance; `StageGeneration` persists output and usage into `ready` before final billing; `SettleGeneration` consumes the saved ready result/usage atomically with final usage billing and permits debt; `FailGeneration` refunds only a still-reserved task. Stable taskRef retries return the saved settled result. Missing usage retains the unit fallback. Ready and settled tasks never refund through failure handlers. A restart can read a ready receipt through ReserveGeneration and settle it without calling the upstream again. After successful upstream work, a staging/settlement DB failure must be retried, never be treated as an upstream failure and refunded. The response-before-stage crash window remains; this reduces duplicate upstream calls but does not promise exactly-once upstream execution. Existing reserved tasks require the host job queue to deduplicate dispatch. New reservations block when balance is zero/negative, including a zero-priced fallback. `BillingPolicy` exposes whether billing is enabled and whether administrators are billed. Caller must check policy and user status before dispatching an upstream call.

`ResolveUserRoute` honours enabled user-selected channel model IDs, then configured logical defaults. `ResolveRoute` routes a logical code, honoring priority and weighted selection. Route credentials are internal only (`APIKey` excluded from JSON). Public model endpoints expose capability labels rather than upstream names. `RecordCall` stores metadata and usage only, never request/response bodies or bearer credentials.

Supported user endpoints: wallet/credits, redeem and wallet/redeem, models unified/available/default (POST), pay packages/create/status/notify/epay/return/epay and authenticated qr.svg. Return is an anonymous 302. QR works without PostgreSQL. Payment create accepts configured packages or a custom amount from 1 to 100000 yuan. Gateway requests occur only on explicit order creation.

Admin endpoints cover overview, users/detail/status/quota/admin role, settings/site/payment settings, ledger adjustments, request/login/model-call logs, payment create/manual settle/refund, codes create/list/disable, channels and channel-model/logical-model save/delete/list, default logical model, route inspection, channel connectivity tests and explicit model import. Paid model tests are deliberately disabled. Storage APIs support configuration, primary/write selection, local/S3-compatible/WebDAV write and write-read-delete probes with mandatory write audit. Set Service.DataRoot before using local storage; os.OpenRoot enforces the root even through symlinks. Native S3 requests use SigV4 and remote redirects are disabled to keep credentials on the configured host. Password/password-reset uses compatible PBKDF2-SHA256 (120000 iterations, 32 bytes) and removes all sessions atomically. Performance reports actual Go memory/PG/request statistics and null for unmeasured CPU/disk data. Queue and render-jobs use host-supplied QueueSnapshot/RenderJobs callbacks. Model enumeration (sync-models GET) performs a bounded, non-generating GET to the channel models endpoint. Cache reload remains an explicit error because this module reads current database values directly. Unknown endpoints return an error, never fabricated success.

PostgreSQL callbacks validate merchant/signature/status/trade ID and amount before acquiring an order transition; settlement then locks the order, enforces provider trade uniqueness, and commits the balance/ledger/order atomically. Closed/refunded orders never reopen. Code redemption locks the code, refuses expired/disabled/other-user-used codes, and is idempotent for the same user (including codes used before this Go migration).

No local JSON financial backend is implemented. A nil pool permits process startup but returns 503 for operations and errors for financial service methods. The caller must not interpret this as free successful billing.

Tests:

```sh
go test ./internal/operations
# Optional actual PostgreSQL row-lock/rollback/concurrency coverage:
OPERATIONS_TEST_DATABASE_URL=postgres://... go test ./internal/operations -run TestPostgresConcurrentTransactions
```

The optional integration test creates and drops an isolated random schema and must point at a disposable testing database. Unit tests cover canonical signing, tamper rejection, exact currency parsing/overflow, settlement state idempotency, nil-pool billing refusal, nested credential redaction, QR and anonymous payment return. No external paid calls or deployment are part of these tests.
