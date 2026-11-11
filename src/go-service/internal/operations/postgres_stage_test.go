package operations

import (
	"encoding/json"
	"testing"
)

func TestPostgresGenerationReadyRecovery(t *testing.T) {
	s, ctx := isolatedService(t)
	if _, err := s.AdjustCredits(ctx, "u", 10000000, "setup", "ready-initial", "", "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReserveGeneration(ctx, "u", "ready-task", 2000000); err != nil {
		t.Fatal(err)
	}
	expected := json.RawMessage(`{"artifact":"saved-image"}`)
	r, err := s.StageGeneration(ctx, "u", "ready-task", 12000000, true, expected)
	if err != nil || r.Status != "ready" || r.ActualMicro != 12000000 || !r.HasUsage {
		t.Fatalf("stage %+v %v", r, err)
	}
	if r, err = s.FailGeneration(ctx, "u", "ready-task"); err != nil || r.Status != "ready" {
		t.Fatalf("ready refunded %+v %v", r, err)
	}
	bal, err := s.Balance(ctx, "u")
	if err != nil || bal.BalanceMicro != 8000000 {
		t.Fatalf("stage changed balance %+v %v", bal, err)
	}
	restarted := New(s.pool, nil)
	if err = restarted.Init(ctx); err != nil {
		t.Fatal(err)
	}
	r, err = restarted.ReserveGeneration(ctx, "u", "ready-task", 2000000)
	if err != nil || r.Status != "ready" || r.ActualMicro != 12000000 {
		t.Fatalf("restart %+v %v", r, err)
	}
	var saved map[string]string
	if err = json.Unmarshal(r.Result, &saved); err != nil || saved["artifact"] != "saved-image" {
		t.Fatal("saved artifact", string(r.Result), err)
	}
	r, err = restarted.StageGeneration(ctx, "u", "ready-task", 0, false, json.RawMessage(`{"artifact":"overwrite"}`))
	if err != nil || r.ActualMicro != 12000000 || !r.HasUsage {
		t.Fatalf("stage overwrite %+v %v", r, err)
	}
	r, err = restarted.SettleGeneration(ctx, "u", "ready-task", 0, false, json.RawMessage(`{"artifact":"wrong"}`))
	if err != nil || r.Status != "settled" || r.ChargedMicro != 12000000 || r.BalanceMicro != -2000000 {
		t.Fatalf("stored usage not authoritative %+v %v", r, err)
	}
	saved = map[string]string{}
	if err = json.Unmarshal(r.Result, &saved); err != nil || saved["artifact"] != "saved-image" {
		t.Fatal("result overwritten", string(r.Result), err)
	}
	r, err = restarted.FailGeneration(ctx, "u", "ready-task")
	if err != nil || r.Status != "settled" {
		t.Fatal(r, err)
	}
	if _, err = restarted.ReserveGeneration(ctx, "u", "new-task", 0); err == nil {
		t.Fatal("debt allowed next task")
	}
}
