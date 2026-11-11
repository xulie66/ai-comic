package operations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func respond(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func success(w http.ResponseWriter, m map[string]any) {
	if m == nil {
		m = map[string]any{}
	}
	m["success"] = true
	respond(w, 200, m)
}
func fail(w http.ResponseWriter, status int, msg string) {
	respond(w, status, map[string]any{"success": false, "error": msg})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) { w.status = code; w.ResponseWriter.WriteHeader(code) }
func (w *statusWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(p)
}
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	sw := &statusWriter{ResponseWriter: w}
	w = sw
	ctx := r.Context()
	who, _ := ctx.Value(identityKey{}).(Identity)
	path := r.URL.Path
	start := time.Now()
	if path == "/api/pay/return/epay" && r.Method == "GET" {
		http.Redirect(w, r, "/ui/?pay=return&out_trade_no="+url.QueryEscape(r.URL.Query().Get("out_trade_no")), http.StatusFound)
		return
	}
	if path == "/api/qr.svg" && r.Method == "GET" {
		if who.ID == "" {
			fail(w, 401, "请先登录")
			return
		}
		s.qrSVG(w, r)
		return
	}
	if s.pool == nil {
		fail(w, 503, "运营数据库未配置，充值与计费暂不可用")
		return
	}
	if path == "/api/pay/notify/epay" {
		s.notify(w, r)
		return
	}
	if who.ID == "" {
		fail(w, 401, "请先登录")
		return
	}
	admin := strings.HasPrefix(path, "/api/admin/")
	if admin && !who.IsAdmin {
		fail(w, 403, "需要管理员身份")
		return
	}
	defer func() {
		logCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		_, err := s.pool.Exec(logCtx, "INSERT INTO request_logs(user_id,method,path,ms,status,kind) VALUES($1,$2,$3,$4,$5,'api')", who.ID, r.Method, path, time.Since(start).Milliseconds(), sw.status)
		if err != nil {
			s.logger.Warn("record request failed", "error", err)
		}
	}()
	b := map[string]any{}
	if r.Method == "POST" || r.Method == "PUT" {
		bodyLimit := int64(1024 * 1024)
		if path == "/api/admin/storage/write" {
			bodyLimit = 45 * 1024 * 1024
		}
		r.Body = http.MaxBytesReader(w, r.Body, bodyLimit)
		dec := json.NewDecoder(r.Body)
		if err := dec.Decode(&b); err != nil {
			fail(w, 400, "请求体不是合法 JSON")
			return
		}
	}
	var result map[string]any
	var err error
	if admin {
		result, err = s.admin(ctx, strings.TrimPrefix(path, "/api/admin/"), r, b, who)
	} else {
		result, err = s.user(ctx, path, r, b, who)
	}
	if err != nil {
		status := 400
		msg := err.Error()
		if errors.Is(err, pgx.ErrNoRows) {
			status = 404
			msg = "记录不存在"
		} else if errors.Is(err, ErrUnavailable) {
			status = 503
		} else if !isPublicError(err) {
			s.logger.Error("operations request failed", "path", path, "error", err)
			status = 500
			msg = "运营操作失败"
		}
		fail(w, status, msg)
		return
	}
	success(w, result)
}
func isPublicError(err error) bool {
	msg := err.Error()
	return !strings.Contains(msg, "SQLSTATE") && !strings.Contains(msg, "conn") && !strings.Contains(msg, "scan") && !strings.Contains(msg, "encode") && !strings.Contains(msg, "decode")
}
func (s *Service) notify(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 256*1024)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "failure", 400)
		return
	}
	p := map[string]string{}
	for k, v := range r.Form {
		if len(v) != 1 {
			http.Error(w, "failure", 400)
			return
		}
		p[k] = v[0]
	}
	settings, err := s.Settings(r.Context())
	if err == nil {
		cfg := object(object(settings, "payment"), "epay")
		var n Notification
		n, err = VerifyNotify(str(cfg, "pid", ""), str(cfg, "key", ""), p)
		if err == nil {
			_, err = s.SettleNotify(r.Context(), n)
		}
	}
	if err != nil {
		http.Error(w, "failure", 400)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("success"))
}
func (s *Service) user(ctx context.Context, path string, r *http.Request, b map[string]any, who Identity) (map[string]any, error) {
	get := r.Method == "GET"
	post := r.Method == "POST"
	switch {
	case (path == "/api/wallet" || path == "/api/credits") && get:
		bal, err := s.Balance(ctx, who.ID)
		if err != nil {
			return nil, err
		}
		ledger, err := s.rows(ctx, "SELECT to_jsonb(l) || jsonb_build_object('balanceAfter',CASE WHEN delta_micro<>0 THEN balance_after/1000000 ELSE balance_after END) FROM credit_ledger l WHERE user_id=$1 ORDER BY at DESC LIMIT 50", who.ID)
		if err != nil {
			return nil, err
		}
		orders, err := s.rows(ctx, `SELECT jsonb_build_object('id',id,'amountYuan',amount,'credits',credits,'status',status,'payType',pay_type,'createdAt',at,'paidAt',paid_at) FROM payments WHERE user_id=$1 ORDER BY at DESC LIMIT 10`, who.ID)
		if err != nil {
			return nil, err
		}
		settings, err := s.Settings(ctx)
		if err != nil {
			return nil, err
		}
		return map[string]any{"balance": bal.Balance, "balanceMicro": bal.BalanceMicro, "items": ledger, "ledger": ledger, "orders": orders, "enabled": boolean(object(settings, "credits"), "enabled", false), "status": "active", "note": str(object(settings, "credits"), "welcomeNote", ""), "canRecharge": boolean(object(settings, "payment"), "enabled", true), "redeemEnabled": boolean(object(settings, "redemption"), "enabled", false)}, nil
	case (path == "/api/redeem" || path == "/api/wallet/redeem") && post:
		settings, err := s.Settings(ctx)
		if err != nil {
			return nil, err
		}
		if !boolean(object(settings, "redemption"), "enabled", false) {
			return nil, errors.New("兑换暂未开启")
		}
		bal, err := s.Redeem(ctx, who.ID, str(b, "code", ""))
		return map[string]any{"balance": bal.Balance, "balanceMicro": bal.BalanceMicro, "duplicated": bal.Duplicated}, err
	case path == "/api/pay/packages" && get:
		packages, err := s.Packages(ctx)
		if err != nil {
			return nil, err
		}
		settings, err := s.Settings(ctx)
		p := object(settings, "payment")
		cfg := object(p, "epay")
		return map[string]any{"packages": packages, "enabled": boolean(p, "enabled", true) && str(cfg, "gateway", "") != "" && str(cfg, "pid", "") != "" && str(cfg, "key", "") != "", "provider": "epay", "payTypes": cfg["payTypes"], "creditsPerYuan": p["creditsPerYuan"], "manualNote": p["manualNote"]}, err
	case path == "/api/pay/create" && post:
		return s.createOrder(ctx, who.ID, b)
	case path == "/api/pay/status" && get:
		items, err := s.rows(ctx, `SELECT jsonb_build_object('id',id,'outTradeNo',trade_no,'amountYuan',amount,'credits',CASE WHEN credits_micro=0 THEN credits ELSE credits_micro::numeric/1000000 END,'status',status,'method',method,'payType',pay_type,'createdAt',at,'paidAt',paid_at,'checkoutUrl',checkout_url) FROM payments WHERE user_id=$1 AND (id=$2 OR ($3<>'' AND trade_no=$3)) LIMIT 1`, who.ID, r.URL.Query().Get("id"), r.URL.Query().Get("out_trade_no"))
		if err != nil {
			return nil, err
		}
		if len(items) == 0 {
			return nil, pgx.ErrNoRows
		}
		bal, err := s.Balance(ctx, who.ID)
		return map[string]any{"order": items[0], "balance": bal.Balance, "balanceMicro": bal.BalanceMicro}, err
	case path == "/api/pay/return/epay" && get:
		return map[string]any{"outTradeNo": r.URL.Query().Get("out_trade_no"), "status": "pending"}, nil
	case path == "/api/models/default" && post:
		cap := str(b, "capability", "text")
		if cap != "image" {
			cap = "text"
		}
		id := str(b, "modelId", "")
		var valid bool
		err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM channel_models cm JOIN channels c ON c.id=cm.channel_id WHERE cm.id=$1 AND cm.capability=$2 AND cm.enabled AND c.enabled)`, id, cap).Scan(&valid)
		if err != nil {
			return nil, err
		}
		if !valid {
			return nil, errors.New("只能选择已启用的对应能力模型")
		}
		_, err = s.pool.Exec(ctx, "UPDATE users SET default_"+cap+"_model_id=$2 WHERE id=$1", who.ID, id)
		return map[string]any{"user": map[string]any{"id": who.ID, "default" + strings.ToUpper(cap[:1]) + cap[1:] + "ModelId": id}}, err
	case path == "/api/models/unified" && get:
		out := map[string]any{"image": nil, "text": nil}
		for _, cap := range []string{"image", "text"} {
			route, err := s.ResolveUserRoute(ctx, who.ID, cap)
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			if err != nil {
				return nil, err
			}
			out[cap] = map[string]any{"id": route.ChannelModelID, "capability": cap, "modelKey": cap, "billingMode": route.BillingMode, "prices": map[string]int64{"unitPriceMicro": route.UnitPriceMicro, "inputTokenPriceMicro": route.InputTokenPriceMicro, "outputTokenPriceMicro": route.OutputTokenPriceMicro, "cachedTokenPriceMicro": route.CachedTokenPriceMicro}}
		}
		return out, nil
	case path == "/api/models/available" && get:
		items, err := s.rows(ctx, `SELECT jsonb_build_object('id',cm.id,'capability',cm.capability,'displayName',cm.display_name,'modelKey',cm.capability,'billingMode',cm.billing_mode,'prices',jsonb_build_object('unitPriceMicro',cm.unit_price_micro,'inputTokenPriceMicro',cm.input_token_price_micro,'outputTokenPriceMicro',cm.output_token_price_micro,'cachedTokenPriceMicro',cm.cached_token_price_micro)) FROM channel_models cm JOIN channels c ON c.id=cm.channel_id WHERE cm.enabled AND c.enabled ORDER BY cm.sort_order,cm.display_name`)
		if err != nil {
			return nil, err
		}
		defs := map[string]string{}
		for _, cap := range []string{"image", "text"} {
			route, e := s.ResolveUserRoute(ctx, who.ID, cap)
			if e == nil {
				defs[cap] = route.ChannelModelID
			}
		}
		return map[string]any{"models": items, "defaults": defs}, nil
	case path == "/api/models/default" && get:
		items, err := s.logicalModels(ctx)
		if err != nil {
			return nil, err
		}
		public := []map[string]any{}
		for _, m := range items {
			if m["enabled"] == true {
				public = append(public, map[string]any{"id": m["id"], "code": m["code"], "name": m["name"], "capability": m["capability"], "icon": m["icon"], "description": m["description"], "isDefault": m["isDefault"], "creditCost": m["creditCost"]})
			}
		}
		settings, err := s.Settings(ctx)
		return map[string]any{"models": public, "defaults": settings["models"], "unified": settings["models"]}, err
	}
	return nil, pgx.ErrNoRows
}
func (s *Service) admin(ctx context.Context, action string, r *http.Request, b map[string]any, who Identity) (map[string]any, error) {
	if result, err, handled := s.extraAdmin(ctx, action, r, b); handled {
		return result, err
	}
	get := r.Method == "GET"
	post := r.Method == "POST"
	q := r.URL.Query()
	switch {
	case action == "overview" && get:
		items, err := s.rows(ctx, `SELECT jsonb_build_object('users',(SELECT count(*) FROM users),'requests24h',(SELECT count(*) FROM request_logs WHERE at>now()-interval '1 day'),'paymentsPending',(SELECT count(*) FROM payments WHERE status='pending'),'revenue',(SELECT COALESCE(sum(amount),0) FROM payments WHERE status='paid'),'creditsMicro',(SELECT COALESCE(sum(credits_micro),0) FROM users))`)
		if err != nil {
			return nil, err
		}
		settings, err := s.Settings(ctx)
		return map[string]any{"overview": items[0], "site": settings["site"], "version": "go"}, err
	case action == "users" && get:
		items, err := s.rows(ctx, `SELECT to_jsonb(u)-'salt'-'hash'-'password'-'password_hash' FROM users u WHERE ($1='' OR username ILIKE '%'||$1||'%') ORDER BY created_at DESC LIMIT 200`, q.Get("q"))
		return map[string]any{"users": items, "defaults": map[string]int{"dailyPages": 80, "pagesPerMinute": 4}}, err
	case strings.HasPrefix(action, "users/") && get:
		items, err := s.rows(ctx, `SELECT to_jsonb(u)-'salt'-'hash'-'password'-'password_hash' FROM users u WHERE id=$1`, strings.TrimPrefix(action, "users/"))
		if err != nil {
			return nil, err
		}
		if len(items) == 0 {
			return nil, pgx.ErrNoRows
		}
		return map[string]any{"user": items[0]}, nil
	case action == "user-status" && post:
		status := str(b, "status", "active")
		if status != "active" && status != "banned" {
			return nil, errors.New("invalid user status")
		}
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return nil, err
		}
		defer tx.Rollback(ctx)
		_, err = tx.Exec(ctx, "UPDATE users SET credit_status=$2,banned_reason=$3 WHERE id=$1", str(b, "userId", ""), status, str(b, "reason", ""))
		if err == nil && status == "banned" {
			_, err = tx.Exec(ctx, "DELETE FROM sessions WHERE user_id=$1", str(b, "userId", ""))
		}
		if err != nil {
			return nil, err
		}
		return nil, tx.Commit(ctx)
	case action == "quota" && post:
		quota := num(b, "dailyPageQuota", 0)
		if quota < 0 || quota > 1000000 {
			return nil, errors.New("每日配额需在0到1000000之间")
		}
		items, err := s.rows(ctx, `UPDATE users SET daily_page_quota=$2 WHERE id=$1 RETURNING to_jsonb(users)-'salt'-'hash'-'password'-'password_hash'`, str(b, "userId", ""), quota)
		if err != nil {
			return nil, err
		}
		if len(items) == 0 {
			return nil, pgx.ErrNoRows
		}
		return map[string]any{"user": items[0]}, nil
	case action == "admin" && post:
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return nil, err
		}
		defer tx.Rollback(ctx)
		if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(19700217)"); err != nil {
			return nil, err
		}
		if !boolean(b, "isAdmin", false) {
			var count int
			var current bool
			if err = tx.QueryRow(ctx, "SELECT is_admin FROM users WHERE id=$1 FOR UPDATE", str(b, "userId", "")).Scan(&current); err != nil {
				return nil, err
			}
			if err = tx.QueryRow(ctx, "SELECT count(*) FROM users WHERE is_admin").Scan(&count); err != nil {
				return nil, err
			}
			if current && count <= 1 {
				return nil, errors.New("至少保留一个管理员")
			}
		}
		users, err := s.rowsInTx(ctx, tx, `UPDATE users SET is_admin=$2 WHERE id=$1 RETURNING to_jsonb(users)-'salt'-'hash'-'password'-'password_hash'`, str(b, "userId", ""), boolean(b, "isAdmin", false))
		if err != nil {
			return nil, err
		}
		if len(users) == 0 {
			return nil, pgx.ErrNoRows
		}
		if err = tx.Commit(ctx); err != nil {
			return nil, err
		}
		return map[string]any{"user": users[0]}, nil
	case (action == "settings" || action == "site" || action == "pay/settings") && get:
		settings, err := s.Settings(ctx)
		if action == "pay/settings" {
			p := object(settings, "payment")
			packages, e := s.Packages(ctx)
			if err == nil {
				err = e
			}
			return map[string]any{"payment": publicSettings(p), "epay": publicSettings(object(p, "epay")), "packages": packages}, err
		}
		return map[string]any{"settings": publicSettings(settings)}, err
	case (action == "settings" || action == "site" || action == "pay/settings") && post:
		current, err := s.Settings(ctx)
		if err != nil {
			return nil, err
		}
		for _, section := range []string{"email", "payment"} {
			incoming := object(b, section)
			if section == "email" {
				if str(incoming, "password", "") == "" {
					delete(incoming, "password")
				}
			} else {
				if ep, ok := incoming["epay"].(map[string]any); ok {
					old := object(object(current, "payment"), "epay")
					key := str(ep, "key", "")
					if key == "" || strings.Contains(key, "*") {
						ep["key"] = old["key"]
					}
					incoming["epay"] = merge(old, ep)
				}
			}
			if len(incoming) > 0 {
				b[section] = merge(object(current, section), incoming)
			}
		}
		settings, err := s.patchSettings(ctx, b)
		return map[string]any{"settings": publicSettings(settings), "payment": publicSettings(settings["payment"])}, err
	case action == "credits/adjust" && post:
		delta := num(b, "deltaMicro", 0)
		if _, ok := b["deltaMicro"]; !ok {
			if v, ok := b["delta"].(float64); ok {
				delta = int64(v * 1e6)
			}
		}
		bal, err := s.AdjustCredits(ctx, str(b, "userId", ""), delta, str(b, "kind", "adjust"), str(b, "ref", ""), str(b, "note", ""), who.ID)
		return map[string]any{"balance": bal.Balance, "balanceMicro": bal.BalanceMicro, "duplicated": bal.Duplicated}, err
	case (action == "models" || action == "channels" || action == "logical-models") && get:
		channels, err := s.channels(ctx)
		if err != nil {
			return nil, err
		}
		models, err := s.logicalModels(ctx)
		return map[string]any{"channels": channels, "models": models}, err
	case action == "models/set-default" && post:
		return s.setDefault(ctx, str(b, "id", ""))
	case action == "logical-models/route" && post:
		route, err := s.ResolveRoute(ctx, str(b, "code", ""), str(b, "capability", "image"))
		return map[string]any{"route": map[string]any{"channel": route.ChannelName, "channelId": route.ChannelID, "modelKey": route.ModelKey, "baseUrl": route.BaseURL}}, err
	case (action == "channels/save" || action == "channel-models/save" || action == "logical-models/save") && post:
		table := strings.ReplaceAll(strings.Split(action, "/")[0], "-", "_")
		id, err := s.saveModel(ctx, table, b)
		if err != nil {
			return nil, err
		}
		channels, err := s.channels(ctx)
		models, e := s.logicalModels(ctx)
		if err == nil {
			err = e
		}
		return map[string]any{"id": id, "channels": channels, "models": models}, err
	case (action == "channels/delete" || action == "channel-models/delete" || action == "logical-models/delete") && post:
		table := strings.ReplaceAll(strings.Split(action, "/")[0], "-", "_")
		err := s.deleteModel(ctx, table, str(b, "id", ""))
		return nil, err
	case action == "codes/create" && post:
		return s.createCodes(ctx, b)
	case action == "codes/disable" && post:
		status := str(b, "status", "disabled")
		if status != "unused" {
			status = "disabled"
		}
		_, err := s.pool.Exec(ctx, "UPDATE redeem_codes SET status=$2 WHERE id=$1 AND status<>'used'", str(b, "id", ""), status)
		return nil, err
	case action == "payments/create" && post:
		return s.manualPayment(ctx, b, who.ID)
	case action == "payments/settle" && post:
		return nil, s.manualSettle(ctx, str(b, "id", ""), str(b, "action", "paid"), who.ID)
	case get:
		table := ""
		order := "at"
		switch action {
		case "requests":
			table = "request_logs"
		case "login-logs":
			table = "login_logs"
		case "credits":
			table = "credit_ledger"
		case "payments":
			table = "payments"
		case "codes":
			table = "redeem_codes"
			order = "created_at"
		case "model-calls":
			table = "model_calls"
		}
		if table != "" {
			limit, _ := strconv.Atoi(q.Get("pageSize"))
			if limit <= 0 || limit > 200 {
				limit = 30
			}
			page, _ := strconv.Atoi(q.Get("page"))
			if page < 1 {
				page = 1
			}
			expr := "to_jsonb(t)"
			if table == "payments" {
				expr = "to_jsonb(t)-'notify_json'"
			}
			if table == "model_calls" {
				expr = "to_jsonb(t)-'request_body'-'response_body'-'error'-'endpoint'"
			}
			where := ""
			args := []any{}
			if q.Get("userId") != "" && table != "redeem_codes" {
				where = " WHERE user_id=$1"
				args = append(args, q.Get("userId"))
			}
			items, err := s.rows(ctx, "SELECT "+expr+" FROM "+table+" t"+where+" ORDER BY "+order+" DESC LIMIT "+strconv.Itoa(limit)+" OFFSET "+strconv.Itoa((page-1)*limit), args...)
			if err != nil {
				return nil, err
			}
			var total int64
			err = s.pool.QueryRow(ctx, "SELECT count(*) FROM "+table+where, args...).Scan(&total)
			return map[string]any{"items": items, "total": total, "page": page, "pageSize": limit}, err
		}
	}
	return nil, fmt.Errorf("unknown operations endpoint")
}
