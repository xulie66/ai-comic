package operations

import (
	"context"
	"crypto/md5"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

func Canonical(params map[string]string) string {
	keys := []string{}
	for k, v := range params {
		if k != "sign" && k != "sign_type" && v != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	parts := []string{}
	for _, k := range keys {
		parts = append(parts, k+"="+params[k])
	}
	return strings.Join(parts, "&")
}
func MD5Sign(p map[string]string, key string) string {
	h := md5.Sum([]byte(Canonical(p) + key))
	return hex.EncodeToString(h[:])
}
func ParseYuanToFen(s string) (int64, error) {
	s = strings.TrimSpace(s)
	p := strings.Split(s, ".")
	if len(p) > 2 || p[0] == "" {
		return 0, errors.New("invalid amount")
	}
	for _, c := range p[0] {
		if c < '0' || c > '9' {
			return 0, errors.New("invalid amount")
		}
	}
	n, err := strconv.ParseInt(p[0], 10, 64)
	if err != nil || n > math.MaxInt64/100 {
		return 0, errors.New("amount overflow")
	}
	fraction := int64(0)
	if len(p) == 2 {
		if len(p[1]) < 1 || len(p[1]) > 2 {
			return 0, errors.New("invalid fraction")
		}
		for _, c := range p[1] {
			if c < '0' || c > '9' {
				return 0, errors.New("invalid fraction")
			}
		}
		f := p[1]
		if len(f) == 1 {
			f += "0"
		}
		fraction, _ = strconv.ParseInt(f, 10, 64)
	}
	if n*100 > math.MaxInt64-fraction {
		return 0, errors.New("amount overflow")
	}
	return n*100 + fraction, nil
}
func formatFen(n int64) string { return fmt.Sprintf("%d.%02d", n/100, n%100) }

type Notification struct {
	OutTradeNo, TradeNo, PayType string
	AmountFen                    int64
}

func VerifyNotify(pid, key string, p map[string]string) (Notification, error) {
	n := Notification{}
	if pid == "" || key == "" || p["pid"] != pid {
		return n, errors.New("merchant mismatch")
	}
	if p["sign_type"] != "" && strings.ToUpper(p["sign_type"]) != "MD5" {
		return n, errors.New("invalid signature type")
	}
	expected := MD5Sign(p, key)
	if subtle.ConstantTimeCompare([]byte(p["sign"]), []byte(expected)) != 1 {
		return n, errors.New("invalid signature")
	}
	fen, err := ParseYuanToFen(p["money"])
	if err != nil || fen <= 0 {
		return n, errors.New("invalid amount")
	}
	n = Notification{strings.TrimSpace(p["out_trade_no"]), strings.TrimSpace(p["trade_no"]), p["type"], fen}
	if n.TradeNo == "" {
		n.TradeNo = p["api_trade_no"]
	}
	if n.OutTradeNo == "" || n.TradeNo == "" || p["trade_status"] != "TRADE_SUCCESS" {
		return n, errors.New("invalid payment notification")
	}
	return n, nil
}
func settlementDecision(status, existingTrade, incomingTrade string, expected, actual int64) (bool, error) {
	if expected != actual {
		return false, errors.New("payment amount mismatch")
	}
	if status == "paid" {
		if existingTrade != "" && existingTrade != incomingTrade {
			return false, errors.New("payment trade mismatch")
		}
		return true, nil
	}
	if status != "pending" && status != "failed" {
		return false, errors.New("payment is closed")
	}
	return false, nil
}
func (s *Service) SettleNotify(ctx context.Context, n Notification) (BalanceResult, error) {
	if s.pool == nil {
		return BalanceResult{}, ErrUnavailable
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return BalanceResult{}, err
	}
	defer tx.Rollback(ctx)
	var id, user, status, trade string
	var fen, micro int64
	err = tx.QueryRow(ctx, `SELECT id,user_id,status,provider_trade_no,round(amount*100)::bigint,CASE WHEN credits_micro=0 THEN (credits*1000000)::bigint ELSE credits_micro END FROM payments WHERE provider='epay' AND trade_no=$1 FOR UPDATE`, n.OutTradeNo).Scan(&id, &user, &status, &trade, &fen, &micro)
	if err != nil {
		return BalanceResult{}, err
	}
	duplicate, err := settlementDecision(status, trade, n.TradeNo, fen, n.AmountFen)
	if err != nil {
		return BalanceResult{}, err
	}
	if duplicate {
		r := BalanceResult{Duplicated: true}
		return r, tx.Commit(ctx)
	}
	if _, err = tx.Exec(ctx, `UPDATE payments SET status='paid',paid_at=now(),provider_trade_no=$2,pay_type=$3,operator='epay' WHERE id=$1`, id, n.TradeNo, n.PayType); err != nil {
		return BalanceResult{}, err
	}
	r, err := adjustTx(ctx, tx, user, micro, "recharge", id, "epay payment", "epay", false)
	if err != nil {
		return r, err
	}
	return r, tx.Commit(ctx)
}
func validHTTP(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && u.User == nil
}
func (s *Service) createOrder(ctx context.Context, user string, b map[string]any) (map[string]any, error) {
	settings, err := s.Settings(ctx)
	if err != nil {
		return nil, err
	}
	pay := object(settings, "payment")
	cfg := object(pay, "epay")
	gateway := strings.TrimRight(str(cfg, "gateway", ""), "/")
	if !boolean(pay, "enabled", true) || str(pay, "provider", "epay") != "epay" || !validHTTP(gateway) || str(cfg, "pid", "") == "" || str(cfg, "key", "") == "" {
		return nil, errors.New("payment is not configured")
	}
	origin := strings.TrimRight(str(cfg, "publicBaseUrl", ""), "/")
	if !validHTTP(origin) {
		return nil, errors.New("payment callback origin is not configured")
	}
	packages, err := s.Packages(ctx)
	if err != nil {
		return nil, err
	}
	var picked map[string]any
	for _, p := range packages {
		if p["id"] == b["packageId"] {
			picked = p
			break
		}
	}
	if picked == nil && b["amountYuan"] != nil {
		fen, parseErr := ParseYuanToFen(fmt.Sprint(b["amountYuan"]))
		if parseErr != nil || fen < 100 || fen > 10000000 {
			return nil, errors.New("充值金额需在1到100000元之间且最多两位小数")
		}
		rate := num(pay, "creditsPerYuan", 10)
		if rate <= 0 || rate > 1000000 {
			return nil, errors.New("invalid credit rate")
		}
		credits := fen * rate / 100
		picked = map[string]any{"amountFen": fen, "creditsMicro": credits * 1000000}
	}
	if picked == nil {
		return nil, errors.New("select a payment package")
	}
	fen, _ := picked["amountFen"].(int64)
	micro, _ := picked["creditsMicro"].(int64)
	payType := str(b, "payType", str(cfg, "payType", "alipay"))
	if payType != "alipay" && payType != "wxpay" && payType != "qqpay" {
		return nil, errors.New("invalid payment type")
	}
	id := newID("pay")
	out := newID("AC")
	params := map[string]string{"pid": str(cfg, "pid", ""), "name": "AI漫画充值", "type": payType, "money": formatFen(fen), "out_trade_no": out, "notify_url": origin + "/api/pay/notify/epay", "return_url": origin + "/ui/?pay=return&out_trade_no=" + out, "sitename": "AI漫画", "clientip": "0.0.0.0", "device": str(cfg, "device", "pc")}
	params["sign"] = MD5Sign(params, str(cfg, "key", ""))
	params["sign_type"] = "MD5"
	tag, err := s.pool.Exec(ctx, `INSERT INTO payments(id,user_id,username,amount,credits,credits_micro,method,provider,pay_type,status,trade_no,operator) SELECT $1,id,username,($3::bigint)::numeric/100,($4::bigint)::numeric/1000000,$4::bigint,'epay','epay',$5,'pending',$6,'self' FROM users WHERE id=$2`, id, user, fen, micro, payType, out)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, errors.New("user not found")
	}
	form := url.Values{}
	for k, v := range params {
		form.Set(k, v)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", gateway+"/mapi.php", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.Client.Do(req)
	if err != nil {
		s.failOrder(ctx, id)
		return nil, errors.New("payment gateway request failed")
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if resp.StatusCode != 200 || json.Unmarshal(raw, &result) != nil || fmt.Sprint(result["code"]) != "1" {
		s.failOrder(ctx, id)
		return nil, errors.New("payment gateway rejected order")
	}
	mode := str(cfg, "checkoutMode", "redirect")
	checkout := str(result, "payurl", "")
	if mode == "qr_code" && str(result, "qrcode", "") != "" {
		checkout = str(result, "qrcode", "")
	}
	if checkout == "" {
		checkout = str(result, "urlscheme", str(result, "qrcode", ""))
	}
	if checkout == "" || (mode != "qr_code" && !validCheckout(checkout)) {
		s.failOrder(ctx, id)
		return nil, errors.New("invalid checkout response")
	}
	_, err = s.pool.Exec(ctx, "UPDATE payments SET checkout_mode=$2,checkout_url=$3 WHERE id=$1", id, mode, checkout)
	return map[string]any{"id": id, "outTradeNo": out, "mode": mode, "checkoutUrl": checkout, "qrPayload": checkout, "openUrl": checkout, "amountYuan": float64(fen) / 100, "credits": float64(micro) / 1e6, "expiresInMinutes": 30}, err
}
func validCheckout(v string) bool {
	u, e := url.Parse(v)
	if e != nil {
		return false
	}
	switch u.Scheme {
	case "https", "http":
		return u.Host != "" && u.User == nil
	case "weixin", "alipays", "alipay", "mqqapi", "qpay":
		return true
	}
	return false
}
func (s *Service) failOrder(ctx context.Context, id string) {
	_, _ = s.pool.Exec(ctx, "UPDATE payments SET status='failed' WHERE id=$1 AND status='pending'", id)
}
func (s *Service) Redeem(ctx context.Context, user, code string) (BalanceResult, error) {
	if s.pool == nil {
		return BalanceResult{}, ErrUnavailable
	}
	code = strings.ToUpper(strings.TrimSpace(code))
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return BalanceResult{}, err
	}
	defer tx.Rollback(ctx)
	var id, status, used string
	var micro int64
	var expiry *time.Time
	err = tx.QueryRow(ctx, "SELECT id,status,used_by,(credits*1000000)::bigint,expires_at FROM redeem_codes WHERE code=$1 FOR UPDATE", code).Scan(&id, &status, &used, &micro, &expiry)
	if err != nil {
		return BalanceResult{}, err
	}
	if status == "used" && used == user {
		var balance int64
		if err := tx.QueryRow(ctx, "SELECT credits_micro FROM users WHERE id=$1", user).Scan(&balance); err != nil {
			return BalanceResult{}, err
		}
		result := balanceResult(balance)
		result.Duplicated = true
		return result, tx.Commit(ctx)
	}
	if status != "unused" || expiry != nil && expiry.Before(time.Now()) || micro <= 0 {
		return BalanceResult{}, errors.New("code is used, disabled or expired")
	}
	if _, err = tx.Exec(ctx, "UPDATE redeem_codes SET status='used',used_by=$2,used_at=now() WHERE id=$1", id, user); err != nil {
		return BalanceResult{}, err
	}
	r, err := adjustTx(ctx, tx, user, micro, "redeem", id, "redeem code", "self", false)
	if err != nil {
		return r, err
	}
	return r, tx.Commit(ctx)
}
