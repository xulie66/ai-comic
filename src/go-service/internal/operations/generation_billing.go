package operations

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"math"
)

type GenerationReceipt struct {
	UserID        string          `json:"userId"`
	TaskRef       string          `json:"taskRef"`
	Status        string          `json:"status"`
	ReservedMicro int64           `json:"reservedMicro"`
	ChargedMicro  int64           `json:"chargedMicro"`
	BalanceMicro  int64           `json:"balanceMicro"`
	Result        json.RawMessage `json:"result"`
	ActualMicro   int64           `json:"actualMicro"`
	HasUsage      bool            `json:"hasUsage"`
}

func generationReceipt(ctx context.Context, tx pgx.Tx, user, ref string) (GenerationReceipt, error) {
	r := GenerationReceipt{}
	err := tx.QueryRow(ctx, "SELECT user_id,task_ref,status,reserved_micro,charged_micro,balance_micro,result,actual_micro,has_usage FROM generation_receipts WHERE user_id=$1 AND task_ref=$2 FOR UPDATE", user, ref).Scan(&r.UserID, &r.TaskRef, &r.Status, &r.ReservedMicro, &r.ChargedMicro, &r.BalanceMicro, &r.Result, &r.ActualMicro, &r.HasUsage)
	return r, err
}

// ReserveGeneration serializes all lifecycle transitions on the user lock, then the receipt lock.
// A positive balance and sufficient unit fallback are required for a new paid generation.
func (s *Service) ReserveGeneration(ctx context.Context, user, ref string, unitMicro int64) (GenerationReceipt, error) {
	if s.pool == nil {
		return GenerationReceipt{}, ErrUnavailable
	}
	if user == "" || ref == "" || len(ref) > 512 || unitMicro < 0 {
		return GenerationReceipt{}, errors.New("invalid generation reservation")
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
	prior, err := generationReceipt(ctx, tx, user, ref)
	if err == nil {
		if prior.ReservedMicro != unitMicro {
			return GenerationReceipt{}, errors.New("generation reference amount mismatch")
		}
		return prior, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return GenerationReceipt{}, err
	}
	if balance <= 0 || balance < unitMicro {
		return GenerationReceipt{}, ErrInsufficient
	}
	adjust, err := adjustTx(ctx, tx, user, -unitMicro, "generation_reserve", ref, "generation unit fallback reservation", "generation", true)
	if err != nil {
		return GenerationReceipt{}, err
	}
	r := GenerationReceipt{UserID: user, TaskRef: ref, Status: "reserved", ReservedMicro: unitMicro, ChargedMicro: unitMicro, BalanceMicro: adjust.BalanceMicro, Result: json.RawMessage("null")}
	if _, err = tx.Exec(ctx, `INSERT INTO generation_receipts(user_id,task_ref,status,reserved_micro,charged_micro,balance_micro) VALUES($1,$2,'reserved',$3::bigint,$3::bigint,$4::bigint)`, user, ref, unitMicro, r.BalanceMicro); err != nil {
		return GenerationReceipt{}, err
	}
	return r, tx.Commit(ctx)
}

// SettleGeneration persists the result and final debit atomically. Actual token cost may
// exceed the balance: the resulting debt blocks the next new reservation.
// Missing usage retains the unit fallback and never causes a zero-cost refund.
func (s *Service) SettleGeneration(ctx context.Context, user, ref string, actualMicro int64, hasUsage bool, result json.RawMessage) (GenerationReceipt, error) {
	if s.pool == nil {
		return GenerationReceipt{}, ErrUnavailable
	}
	if ref == "" || actualMicro < 0 {
		return GenerationReceipt{}, errors.New("invalid generation settlement")
	}
	if len(result) == 0 {
		result = json.RawMessage("null")
	}
	if len(result) > 32*1024*1024 || !json.Valid(result) {
		return GenerationReceipt{}, errors.New("invalid generation result")
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
	if r.Status == "settled" {
		return r, tx.Commit(ctx)
	}
	if r.Status != "reserved" && r.Status != "ready" {
		return r, errors.New("generation reservation is closed")
	}
	if r.Status == "ready" {
		actualMicro = r.ActualMicro
		hasUsage = r.HasUsage
		result = r.Result
	}
	charge := r.ReservedMicro
	if hasUsage {
		charge = actualMicro
	}
	delta := r.ReservedMicro - charge
	if delta < 0 && balance < math.MinInt64-delta {
		return r, errors.New("generation credit overflow")
	}
	adjust, err := adjustTx(ctx, tx, user, delta, "generation_settle", ref, "generation final usage", "generation", false)
	if err != nil {
		return r, err
	}
	if _, err = tx.Exec(ctx, "UPDATE generation_receipts SET status='settled',charged_micro=$3::bigint,balance_micro=$4::bigint,result=$5::jsonb,actual_micro=$6::bigint,has_usage=$7::boolean,updated_at=now() WHERE user_id=$1 AND task_ref=$2", user, ref, charge, adjust.BalanceMicro, []byte(result), actualMicro, hasUsage); err != nil {
		return r, err
	}
	r.Status = "settled"
	r.ActualMicro = actualMicro
	r.HasUsage = hasUsage
	r.ChargedMicro = charge
	r.BalanceMicro = adjust.BalanceMicro
	r.Result = append(json.RawMessage(nil), result...)
	return r, tx.Commit(ctx)
}

// FailGeneration refunds only a still-reserved task. A settled task is immutable.
func (s *Service) FailGeneration(ctx context.Context, user, ref string) (GenerationReceipt, error) {
	if s.pool == nil {
		return GenerationReceipt{}, ErrUnavailable
	}
	if ref == "" {
		return GenerationReceipt{}, errors.New("missing generation reference")
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
	if r.Status == "failed" || r.Status == "settled" || r.Status == "ready" {
		return r, tx.Commit(ctx)
	}
	adjust, err := adjustTx(ctx, tx, user, r.ReservedMicro, "generation_fail", ref, "failed generation refund", "generation", false)
	if err != nil {
		return r, err
	}
	if _, err = tx.Exec(ctx, "UPDATE generation_receipts SET status='failed',charged_micro=0,balance_micro=$3::bigint,updated_at=now() WHERE user_id=$1 AND task_ref=$2", user, ref, adjust.BalanceMicro); err != nil {
		return r, err
	}
	r.Status = "failed"
	r.ChargedMicro = 0
	r.BalanceMicro = adjust.BalanceMicro
	return r, tx.Commit(ctx)
}
