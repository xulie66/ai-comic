package operations

import (
	"regexp"
	"strings"
	"testing"
)

func TestSchemaRequiredMigrationsAreExecutable(t *testing.T) {
	required := []string{"ALTER TABLE users ADD COLUMN IF NOT EXISTS credits_micro BIGINT NOT NULL DEFAULT 0;", "ALTER TABLE credit_ledger ADD COLUMN IF NOT EXISTS delta_micro BIGINT NOT NULL DEFAULT 0;", "ALTER TABLE logical_models ADD COLUMN IF NOT EXISTS is_default BOOLEAN NOT NULL DEFAULT false;", "ALTER TABLE settings ADD COLUMN IF NOT EXISTS key TEXT;"}
	executable := strings.Builder{}
	for _, line := range strings.Split(schema, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "\ufeff"))
		if strings.HasPrefix(line, "--") {
			if regexp.MustCompile(`(?i)\b(ALTER TABLE|CREATE TABLE)\b`).MatchString(line) {
				t.Fatalf("migration accidentally commented out: %s", line)
			}
			continue
		}
		executable.WriteString(line)
		executable.WriteByte('\n')
	}
	for _, statement := range required {
		if !strings.Contains(executable.String(), statement) {
			t.Fatalf("required executable migration missing: %s", statement)
		}
	}
	for _, r := range schema {
		if r > 127 && r != '\ufeff' {
			t.Fatalf("schema must stay ASCII to avoid platform decoding damage: %U", r)
		}
	}
}
