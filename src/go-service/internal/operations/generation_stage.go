package operations

import (
	"context"
	"encoding/json"
	"errors"
)

// StageGeneration durably saves valid upstream output before final billing.
// This narrows the upstream-response crash window; it does not guarantee upstream exactly-once.
func (s *Service) StageGeneration(ctx context.Context, user, ref string, actualMicro int64, hasUsage bool, result json.RawMessage) (GenerationReceipt, error) {
	if s.pool == nil {
		return GenerationReceipt{}, ErrUnavailable
	}
	if user == "" || ref == "" || actualMicro < 0 || len(result) == 0 || len(result) > 32*1024*1024 || !json.Valid(result) {
		return GenerationReceipt{}, errors.New("invalid generation staging")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return GenerationReceipt{}, err
	}
	defer tx.Rollback(ctx)
	var balance int64
	if err = tx.QueryRow(ctx, "SELECT credits_micro FROM users WHERE id=$1 FOR UPDATE", user).Scan(&balance); err != nil {
		return GenerationReceipt{}, err
	}
	r, err := generationReceipt(ctx, tx, user, ref)
	if err != nil {
		return r, err
	}
	if r.Status == "ready" || r.Status == "settled" {
		return r, tx.Commit(ctx)
	}
	if r.Status != "reserved" {
		return r, errors.New("generation reservation is closed")
	}
	if _, err = tx.Exec(ctx, "UPDATE generation_receipts SET status='ready',actual_micro=$3::bigint,has_usage=$4::boolean,result=$5::jsonb,updated_at=now() WHERE user_id=$1 AND task_ref=$2", user, ref, actualMicro, hasUsage, []byte(result)); err != nil {
		return r, err
	}
	r.Status = "ready"
	r.ActualMicro = actualMicro
	r.HasUsage = hasUsage
	r.Result = append(json.RawMessage(nil), result...)
	return r, tx.Commit(ctx)
}
