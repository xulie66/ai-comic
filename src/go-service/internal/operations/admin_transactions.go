package operations

import (
	"context"
	"errors"
	"time"
)

func (s *Service) createCodes(ctx context.Context, b map[string]any) (map[string]any, error) {
	count := num(b, "count", 10)
	credits := num(b, "credits", 10)
	if count < 1 || count > 500 || credits <= 0 || credits > 1000000000 {
		return nil, errors.New("invalid code count or credits")
	}
	var expiry any
	if text := str(b, "expiresAt", ""); text != "" {
		t, err := time.Parse(time.RFC3339, text)
		if err != nil {
			return nil, errors.New("invalid expiration timestamp")
		}
		expiry = t
	}
	batch := str(b, "batch", newID("batch"))
	prefix := str(b, "prefix", "MANGA")
	if len(prefix) > 32 {
		return nil, errors.New("code prefix too long")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	codes := []string{}
	for i := int64(0); i < count; i++ {
		code := prefix + "-" + newID("RC")
		if _, err = tx.Exec(ctx, "INSERT INTO redeem_codes(id,code,credits,batch,expires_at,note) VALUES($1,upper($2),$3,$4,$5,$6)", newID("rc"), code, credits, batch, expiry, str(b, "note", "")); err != nil {
			return nil, err
		}
		codes = append(codes, code)
	}
	return map[string]any{"codes": codes, "count": len(codes), "batch": batch, "credits": credits}, tx.Commit(ctx)
}
func (s *Service) manualPayment(ctx context.Context, b map[string]any, operator string) (map[string]any, error) {
	amount := num(b, "amount", 0)
	credits := num(b, "credits", 0)
	if amount <= 0 || credits <= 0 || credits > 1000000000 {
		return nil, errors.New("invalid payment amount or credits")
	}
	id := newID("pay")
	tag, err := s.pool.Exec(ctx, `INSERT INTO payments(id,user_id,username,amount,credits,credits_micro,method,provider,status,trade_no,note,operator) SELECT $1,id,username,($3::bigint)::numeric,($4::bigint)::numeric,$4::bigint*1000000,'manual','manual','pending',$5,$6,$7 FROM users WHERE id=$2`, id, str(b, "userId", ""), amount, credits, str(b, "tradeNo", ""), str(b, "note", ""), operator)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, errors.New("user not found")
	}
	if str(b, "status", "pending") == "paid" {
		err = s.manualSettle(ctx, id, "paid", operator)
	}
	return map[string]any{"id": id}, err
}
func (s *Service) manualSettle(ctx context.Context, id, action, operator string) error {
	if action != "paid" && action != "refund" && action != "cancelled" {
		return errors.New("invalid settlement action")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var user, status string
	var micro int64
	if err = tx.QueryRow(ctx, "SELECT user_id,status,CASE WHEN credits_micro=0 THEN (credits*1000000)::bigint ELSE credits_micro END FROM payments WHERE id=$1 FOR UPDATE", id).Scan(&user, &status, &micro); err != nil {
		return err
	}
	if action == "paid" {
		if status == "paid" {
			return tx.Commit(ctx)
		}
		if status != "pending" && status != "failed" {
			return errors.New("payment is closed")
		}
		if _, err = adjustTx(ctx, tx, user, micro, "recharge", id, "manual payment", operator, false); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, "UPDATE payments SET status='paid',paid_at=now(),operator=$2 WHERE id=$1", id, operator)
	} else if action == "refund" {
		if status == "refunded" {
			return tx.Commit(ctx)
		}
		if status != "paid" {
			return errors.New("only paid orders can be refunded")
		}
		if _, err = adjustTx(ctx, tx, user, -micro, "refund", id, "manual credit reversal", operator, false); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, "UPDATE payments SET status='refunded',operator=$2 WHERE id=$1", id, operator)
	} else {
		if status == "paid" || status == "refunded" {
			return errors.New("payment is already settled")
		}
		_, err = tx.Exec(ctx, "UPDATE payments SET status='cancelled',operator=$2 WHERE id=$1", id, operator)
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
