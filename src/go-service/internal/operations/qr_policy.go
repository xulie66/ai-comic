package operations

import (
	"context"
	"fmt"
	qrcode "github.com/skip2/go-qrcode"
	"net/http"
	"strconv"
	"strings"
)

type BillingPolicy struct {
	Enabled     bool
	ChargeAdmin bool
}

func (s *Service) BillingPolicy(ctx context.Context) (BillingPolicy, error) {
	settings, err := s.Settings(ctx)
	if err != nil {
		return BillingPolicy{}, err
	}
	credits := object(settings, "credits")
	return BillingPolicy{Enabled: boolean(credits, "enabled", false), ChargeAdmin: boolean(credits, "chargeAdmin", true)}, nil
}
func (s *Service) qrSVG(w http.ResponseWriter, r *http.Request) {
	text := r.URL.Query().Get("text")
	if len(text) == 0 || len(text) > 2048 {
		fail(w, 400, "二维码内容为空或过长")
		return
	}
	qr, err := qrcode.New(text, qrcode.Medium)
	if err != nil {
		fail(w, 400, "二维码内容过长")
		return
	}
	size, _ := strconv.Atoi(r.URL.Query().Get("size"))
	if size < 120 {
		size = 280
	}
	if size > 720 {
		size = 720
	}
	bits := qr.Bitmap()
	n := len(bits)
	var path strings.Builder
	for y, row := range bits {
		for x, on := range row {
			if on {
				fmt.Fprintf(&path, "M%d %dh1v1h-1z", x, y)
			}
		}
	}
	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	w.Header().Set("Cache-Control", "private, max-age=300")
	fmt.Fprintf(w, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d" shape-rendering="crispEdges"><rect width="100%%" height="100%%" fill="white"/><path d="%s" fill="black"/></svg>`, size, size, n, n, path.String())
}
