package operations

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
)

type RegistrationPolicy struct {
	RegisterOpen   bool   `json:"registerOpen"`
	InviteRequired bool   `json:"inviteRequired"`
	InviteCode     string `json:"-"`
}

// RegistrationPolicy reads explicitly persisted site configuration; defaults never open registration.
func (s *Service) RegistrationPolicy(ctx context.Context) (RegistrationPolicy, error) {
	p := RegistrationPolicy{InviteRequired: true}
	if s.pool == nil {
		return p, ErrUnavailable
	}
	var raw []byte
	err := s.pool.QueryRow(ctx, "SELECT value FROM settings WHERE key='site'").Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	var site map[string]any
	if err = json.Unmarshal(raw, &site); err != nil {
		return p, err
	}
	p.RegisterOpen = boolean(site, "registerOpen", false)
	p.InviteRequired = boolean(site, "inviteRequired", true)
	p.InviteCode = str(site, "inviteCode", "")
	return p, nil
}
