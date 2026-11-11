package operations

import (
	"testing"
)

func TestAdminDatabaseCamelFields(t *testing.T) {
	row := camelMap(map[string]any{"base_url": "https://example.test", "timeout_ms": int64(60000), "model_key": "internal", "unit_price_micro": int64(7), "channel_model_id": "cm", "last_check_ok": true, "apiKeyMasked": "********"})
	for key, want := range map[string]any{"baseUrl": "https://example.test", "timeoutMs": int64(60000), "modelKey": "internal", "unitPriceMicro": int64(7), "channelModelId": "cm", "lastCheckOk": true, "apiKeyMasked": "********"} {
		if row[key] != want {
			t.Fatalf("%s: %+v", key, row)
		}
	}
}
