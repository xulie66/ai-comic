package operations

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
)

// ResolveUserRoute honours enabled user-selected channel models before site defaults.
func (s *Service) ResolveUserRoute(ctx context.Context, user, cap string) (Route, error) {
	r := Route{}
	if s.pool == nil {
		return r, ErrUnavailable
	}
	if cap != "image" && cap != "text" {
		return r, errors.New("invalid capability")
	}
	err := s.pool.QueryRow(ctx, `SELECT c.id,cm.id,c.name,c.base_url,c.api_key,CASE WHEN cm.upstream_model_key<>'' THEN cm.upstream_model_key ELSE cm.model_key END,cm.protocol,c.provider,cm.capability,c.timeout_ms,cm.billing_mode,cm.unit_price_micro,cm.input_token_price_micro,cm.output_token_price_micro,cm.cached_token_price_micro FROM users u JOIN channel_models cm ON cm.id=u.default_`+cap+`_model_id JOIN channels c ON c.id=cm.channel_id WHERE u.id=$1 AND cm.capability=$2 AND cm.enabled AND c.enabled`, user, cap).Scan(&r.ChannelID, &r.ChannelModelID, &r.ChannelName, &r.BaseURL, &r.APIKey, &r.ModelKey, &r.Protocol, &r.Provider, &r.Capability, &r.TimeoutMS, &r.BillingMode, &r.UnitPriceMicro, &r.InputTokenPriceMicro, &r.OutputTokenPriceMicro, &r.CachedTokenPriceMicro)
	if errors.Is(err, pgx.ErrNoRows) {
		return s.ResolveRoute(ctx, "", cap)
	}
	return r, err
}
