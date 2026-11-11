package operations

import (
	"encoding/json"
	"sync"
	"testing"
)

func TestPostgresGenerationLifecycle(t *testing.T) {
	s, ctx := isolatedService(t)
	if _, err := s.AdjustCredits(ctx, "u", 10000000, "setup", "initial", "", "test"); err != nil {
		t.Fatal(err)
	}
	concurrent := func(f func() error) {
		t.Helper()
		var wg sync.WaitGroup
		errs := make(chan error, 16)
		for i := 0; i < 16; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); errs <- f() }()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	concurrent(func() error { _, err := s.ReserveGeneration(ctx, "u", "task-1", 2000000); return err })
	bal, err := s.Balance(ctx, "u")
	if err != nil || bal.BalanceMicro != 8000000 {
		t.Fatalf("reserve %+v %v", bal, err)
	}
	result := json.RawMessage(`{"image":"artifact-1"}`)
	concurrent(func() error { _, err := s.SettleGeneration(ctx, "u", "task-1", 12000000, true, result); return err })
	bal, err = s.Balance(ctx, "u")
	if err != nil || bal.BalanceMicro != -2000000 {
		t.Fatalf("debt %+v %v", bal, err)
	}
	if _, err = s.ReserveGeneration(ctx, "u", "task-2", 0); err == nil {
		t.Fatal("debt allowed next generation")
	}
	receipt, err := s.ReserveGeneration(ctx, "u", "task-1", 2000000)
	if err != nil || receipt.Status != "settled" || receipt.ChargedMicro != 12000000 {
		t.Fatalf("retry %+v %v", receipt, err)
	}
	var saved map[string]any
	if err = json.Unmarshal(receipt.Result, &saved); err != nil || saved["image"] != "artifact-1" {
		t.Fatal("stored result", string(receipt.Result), err)
	}
	receipt, err = s.FailGeneration(ctx, "u", "task-1")
	if err != nil || receipt.Status != "settled" {
		t.Fatal("settled refunded", receipt, err)
	}
	if _, err = s.AdjustCredits(ctx, "u", 10000000, "setup", "topup", "", "test"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReserveGeneration(ctx, "u", "missing-usage", 2000000); err != nil {
		t.Fatal(err)
	}
	receipt, err = s.SettleGeneration(ctx, "u", "missing-usage", 0, false, json.RawMessage(`{"ok":true}`))
	if err != nil || receipt.ChargedMicro != 2000000 || receipt.BalanceMicro != 6000000 {
		t.Fatalf("missing usage free %+v %v", receipt, err)
	}
	if _, err = s.ReserveGeneration(ctx, "u", "failed-task", 1000000); err != nil {
		t.Fatal(err)
	}
	concurrent(func() error { _, err := s.FailGeneration(ctx, "u", "failed-task"); return err })
	bal, err = s.Balance(ctx, "u")
	if err != nil || bal.BalanceMicro != 6000000 {
		t.Fatalf("refund %+v %v", bal, err)
	}
	if _, err = s.SettleGeneration(ctx, "u", "failed-task", 0, true, nil); err == nil {
		t.Fatal("failed reopened")
	}
	if _, err = s.ReserveGeneration(ctx, "u", "failed-task", 2000000); err == nil {
		t.Fatal("same ref changed reserve")
	}
	policy, err := s.RegistrationPolicy(ctx)
	if err != nil || policy.RegisterOpen {
		t.Fatalf("default registration %+v %v", policy, err)
	}
	if _, err = s.patchSettings(ctx, map[string]any{"site": map[string]any{"registerOpen": true, "inviteRequired": true, "inviteCode": "SECRET-CODE"}}); err != nil {
		t.Fatal(err)
	}
	policy, err = s.RegistrationPolicy(ctx)
	if err != nil || !policy.RegisterOpen || policy.InviteCode != "SECRET-CODE" {
		t.Fatalf("registration %+v %v", policy, err)
	}
	raw, _ := json.Marshal(policy)
	if contains(string(raw), "SECRET-CODE") {
		t.Fatal("invite leaked")
	}
}
