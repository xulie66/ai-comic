package httpapi

import (
	"comic.local/go-service/internal/store"
	"fmt"
)

func pageIndex(page map[string]any) (int, error) {
	v := page["page_index"]
	if v == nil {
		v = page["index"]
	}
	var n int
	switch x := v.(type) {
	case float64:
		n = int(x)
		if float64(n) != x {
			return 0, store.ErrInvalid
		}
	case int:
		n = x
	case string:
		if _, e := fmt.Sscan(x, &n); e != nil {
			return 0, store.ErrInvalid
		}
	default:
		return 0, store.ErrInvalid
	}
	if n < 1 {
		return 0, store.ErrInvalid
	}
	return n, nil
}
func (s *Server) renderSnapshot(uid, pid string) any {
	if s.Jobs == nil {
		return map[string]any{"state": "idle"}
	}
	return s.Jobs.Status(uid, pid)
}
func pageImage(p map[string]any) string {
	return first(str(p, "page_image_url"), first(str(p, "imagePath"), str(p, "pageImageUrl")))
}
