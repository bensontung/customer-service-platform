package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/example/customer-service-platform/pkg/idempotency"
	"golang.org/x/time/rate"
)

type RequestLimitsMiddleware struct {
	mu      sync.Mutex
	clients map[string]*clientLimiter
}
type clientLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

func NewRequestLimitsMiddleware() *RequestLimitsMiddleware {
	return &RequestLimitsMiddleware{clients: map[string]*clientLimiter{}}
}
func (m *RequestLimitsMiddleware) Handle(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.RemoteAddr
		m.mu.Lock()
		client := m.clients[key]
		if client == nil {
			client = &clientLimiter{limiter: rate.NewLimiter(20, 40)}
			m.clients[key] = client
		}
		client.lastSeen = time.Now()
		if len(m.clients) > 10000 {
			for k, v := range m.clients {
				if time.Since(v.lastSeen) > 10*time.Minute {
					delete(m.clients, k)
				}
			}
		}
		limiter := client.limiter
		m.mu.Unlock()
		if !limiter.Allow() {
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		idempotency.Middleware(next)(w, r)
	}
}
