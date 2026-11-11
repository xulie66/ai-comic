package operations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/pbkdf2"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func isolatedService(t *testing.T) (*Service, context.Context) {
	t.Helper()
	dsn := os.Getenv("OPERATIONS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set OPERATIONS_TEST_DATABASE_URL for actual PostgreSQL operations SQL")
	}
	ctx := context.Background()
	root, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	name := newID("ops_crud")
	if _, err = root.Exec(ctx, "CREATE SCHEMA "+name); err != nil {
		root.Close()
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = name
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close(); root.Exec(ctx, "DROP SCHEMA "+name+" CASCADE"); root.Close() })
	_, err = pool.Exec(ctx, `CREATE TABLE users(id TEXT PRIMARY KEY,username TEXT,display_name TEXT DEFAULT '',salt TEXT DEFAULT '',hash TEXT DEFAULT '',is_admin BOOLEAN DEFAULT false,daily_page_quota INTEGER DEFAULT 0,created_at TIMESTAMPTZ DEFAULT now(),default_image_model_id TEXT DEFAULT '',default_text_model_id TEXT DEFAULT '',credits NUMERIC DEFAULT 0); CREATE TABLE sessions(token TEXT PRIMARY KEY,user_id TEXT); INSERT INTO users(id,username,is_admin) VALUES('admin','admin',true),('u','user',false)`)
	if err != nil {
		t.Fatal(err)
	}
	s := New(pool, nil)
	s.DataRoot = t.TempDir()
	if err = s.Init(ctx); err != nil {
		t.Fatal(err)
	}
	return s, ctx
}
func TestPostgresModelCRUDAndUserRoutes(t *testing.T) {
	s, ctx := isolatedService(t)
	channel, err := s.saveModel(ctx, "channels", map[string]any{"name": "private", "baseUrl": "https://upstream.example.test/v1", "apiKey": "SECRET"})
	if err != nil {
		t.Fatal(err)
	}
	cm, err := s.saveModel(ctx, "channel_models", map[string]any{"channelId": channel, "modelKey": "private-upstream", "capability": "image", "unitPriceMicro": float64(17)})
	if err != nil {
		t.Fatal(err)
	}
	lm, err := s.saveModel(ctx, "logical_models", map[string]any{"code": "manga-image-default", "name": "公开图片", "capability": "image", "routes": []any{map[string]any{"channelModelId": cm, "priority": float64(1), "weight": float64(3)}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.setDefault(ctx, lm); err != nil {
		t.Fatal(err)
	}
	route, err := s.ResolveRoute(ctx, "", "image")
	if err != nil || route.ChannelModelID != cm || route.UnitPriceMicro != 17 || route.APIKey != "SECRET" {
		t.Fatalf("%+v %v", route, err)
	}
	models, err := s.logicalModels(ctx)
	if err != nil || len(models) != 1 {
		t.Fatalf("%+v %v", models, err)
	}
	channels, err := s.channels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(channels)
	if strings.Contains(string(raw), "SECRET") {
		t.Fatal("channel credential leaked")
	}
	ch := channels[0]
	for _, key := range []string{"baseUrl", "timeoutMs", "sortOrder", "lastCheckAt", "lastCheckOk", "lastCheckMsg", "lastCheckMs", "createdAt", "updatedAt", "hasKey", "apiKeyMasked"} {
		if _, ok := ch[key]; !ok {
			t.Fatalf("Channels.vue channel field %s missing: %+v", key, ch)
		}
	}
	if ch["apiKeyMasked"] != "********" || ch["hasKey"] != true || ch["apiKey"] != "" {
		t.Fatalf("key DTO %+v", ch)
	}
	nested := ch["models"].([]map[string]any)[0]
	for _, key := range []string{"channelId", "modelKey", "displayName", "billingMode", "unitPriceMicro", "inputTokenPriceMicro", "outputTokenPriceMicro", "cachedTokenPriceMicro", "upstreamModelKey", "sortOrder", "lastCheckAt", "lastCheckOk", "lastCheckMsg"} {
		if _, ok := nested[key]; !ok {
			t.Fatalf("Channels.vue model field %s missing: %+v", key, nested)
		}
	}
	if nested["modelKey"] != "private-upstream" || nested["unitPriceMicro"] != float64(17) {
		t.Fatalf("nested DTO %+v", nested)
	}
	routes := models[0]["routes"].([]map[string]any)
	if len(routes) != 1 || routes[0]["channelModelId"] != cm || routes[0]["channelId"] != channel {
		t.Fatalf("logical routes DTO %+v", routes)
	}
	for _, key := range []string{"base_url", "timeout_ms", "model_key", "unit_price_micro", "channel_model_id"} {
		if strings.Contains(string(raw), `"`+key+`"`) {
			t.Fatalf("snake field leaked: %s", key)
		}
	}
	req := httptest.NewRequest("GET", "/api/admin/channels", nil)
	req = req.WithContext(WithIdentity(ctx, "admin", true))
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"baseUrl"`) || !strings.Contains(rec.Body.String(), `"unitPriceMicro":17`) {
		t.Fatalf("http DTO %d %s", rec.Code, rec.Body.String())
	}
	publicReq := httptest.NewRequest("GET", "/api/models/available", nil)
	publicReq = publicReq.WithContext(WithIdentity(ctx, "u", false))
	publicRec := httptest.NewRecorder()
	s.ServeHTTP(publicRec, publicReq)
	if publicRec.Code != 200 {
		t.Fatal(publicRec.Code, publicRec.Body.String())
	}
	for _, private := range []string{"private-upstream", "upstream.example.test", "SECRET"} {
		if strings.Contains(publicRec.Body.String(), private) {
			t.Fatalf("public model leaked %s: %s", private, publicRec.Body.String())
		}
	}
	if _, err = s.saveModel(ctx, "channels", map[string]any{"id": channel, "name": "updated", "baseUrl": "https://upstream.example.test/v1", "apiKey": ""}); err != nil {
		t.Fatal(err)
	}
	selected, err := s.saveModel(ctx, "channel_models", map[string]any{"channelId": channel, "modelKey": "user-upstream", "capability": "image", "unitPriceMicro": float64(23)})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("POST", "/api/models/default", nil)
	if _, err = s.user(ctx, "/api/models/default", request, map[string]any{"modelId": selected, "capability": "image"}, Identity{ID: "u"}); err != nil {
		t.Fatal(err)
	}
	route, err = s.ResolveUserRoute(ctx, "u", "image")
	if err != nil || route.ChannelModelID != selected || route.APIKey != "SECRET" {
		t.Fatalf("%+v %v", route, err)
	}
	if _, err = s.pool.Exec(ctx, "UPDATE channel_models SET enabled=false WHERE id=$1", selected); err != nil {
		t.Fatal(err)
	}
	route, err = s.ResolveUserRoute(ctx, "u", "image")
	if err != nil || route.ChannelModelID != cm {
		t.Fatalf("fallback %+v %v", route, err)
	}
	if err = s.deleteModel(ctx, "logical_models", lm); err != nil {
		t.Fatal(err)
	}
	if err = s.deleteModel(ctx, "channels", channel); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM model_routes)+(SELECT count(*) FROM channel_models)+(SELECT count(*) FROM channels)").Scan(&count); err != nil || count != 0 {
		t.Fatalf("orphans %d %v", count, err)
	}
}
func TestPostgresStorageAndAdminMutations(t *testing.T) {
	s, ctx := isolatedService(t)
	req := httptest.NewRequest("POST", "/api/admin/storage/save", nil)
	out, err := s.storageAdmin(ctx, "storage/save", req, map[string]any{"name": "test", "kind": "local", "rootPrefix": "objects", "writeEnabled": true})
	if err != nil {
		t.Fatal(err)
	}
	id := out["id"].(string)
	if _, err = s.storageAdmin(ctx, "storage/write", req, map[string]any{"ids": []any{id}, "key": "a/b.txt", "text": "hello"}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.storageAdmin(ctx, "storage/test", req, map[string]any{"ids": []any{id}}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = s.pool.QueryRow(ctx, "SELECT count(*) FROM storage_writes WHERE target_id=$1 AND ok", id).Scan(&count); err != nil || count != 2 {
		t.Fatalf("audit %d %v", count, err)
	}
	if _, err = s.storageAdmin(ctx, "storage/write-targets", req, map[string]any{"ids": []any{id}, "writeEnabled": true, "makePrimary": true}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.storageAdmin(ctx, "storage/delete", req, map[string]any{"id": id}); err == nil {
		t.Fatal("deleted primary")
	}
	if _, err = s.pool.Exec(ctx, "INSERT INTO sessions VALUES('token','u')"); err != nil {
		t.Fatal(err)
	}
	if _, err, handled := s.systemAdmin(ctx, "password", req, map[string]any{"userId": "u", "password": "new-password"}); err != nil || !handled {
		t.Fatal(err)
	}
	if err = s.pool.QueryRow(ctx, "SELECT count(*) FROM sessions WHERE user_id='u'").Scan(&count); err != nil || count != 0 {
		t.Fatal("sessions survived", err)
	}
	var saltHex, hashHex string
	if err = s.pool.QueryRow(ctx, "SELECT salt,hash FROM users WHERE id='u'").Scan(&saltHex, &hashHex); err != nil {
		t.Fatal(err)
	}
	salt, err := hex.DecodeString(saltHex)
	if err != nil {
		t.Fatal(err)
	}
	derived := hex.EncodeToString(pbkdf2.Key([]byte("new-password"), salt, 120000, 32, sha256.New))
	if derived != hashHex {
		t.Fatal("incompatible password hash")
	}
	if _, err = s.admin(ctx, "admin", req, map[string]any{"userId": "admin", "isAdmin": false}, Identity{ID: "admin", IsAdmin: true}); err == nil {
		t.Fatal("last admin demoted")
	}
	if _, err = s.admin(ctx, "quota", req, map[string]any{"userId": "u", "dailyPageQuota": float64(42)}, Identity{ID: "admin", IsAdmin: true}); err != nil {
		t.Fatal(err)
	}
	perf := httptest.NewRequest(http.MethodGet, "/api/admin/performance", nil)
	if _, err, handled := s.systemAdmin(ctx, "performance", perf, nil); err != nil || !handled {
		t.Fatal("performance", err)
	}
}
