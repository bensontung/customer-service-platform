package idempotency

import (
	"context"
	"net/http"
)

type contextKey string

const headerKey contextKey = "idempotency-key"

func Middleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		next(w, r.WithContext(context.WithValue(r.Context(), headerKey, r.Header.Get("Idempotency-Key"))))
	}
}
func FromContext(ctx context.Context) string { value, _ := ctx.Value(headerKey).(string); return value }
