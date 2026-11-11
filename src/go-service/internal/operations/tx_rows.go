package operations

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
)

func (s *Service) rowsInTx(ctx context.Context, tx pgx.Tx, q string, args ...any) ([]map[string]any, error) {
	rows, err := tx.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var item map[string]any
		if err := json.Unmarshal(raw, &item); err != nil {
			return nil, err
		}
		items = append(items, camelMap(item))
	}
	return items, rows.Err()
}
