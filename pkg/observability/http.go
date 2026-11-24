package observability

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/time/rate"
)

var requests = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "customer_service_http_request_seconds", Help: "HTTP request duration", Buckets: []float64{.01, .05, .1, .25, .5, 1, 2}}, []string{"method", "status"})
var external = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "customer_service_external_errors_total", Help: "External adapter failures"}, []string{"platform", "operation"})
var outbox = prometheus.NewGauge(prometheus.GaugeOpts{Name: "customer_service_outbox_lag_seconds", Help: "Oldest unhandled outbox event lag"})

type contextKey string

const TraceKey contextKey = "trace_id"

type writer struct {
	http.ResponseWriter
	status int
}

func (w *writer) WriteHeader(status int) { w.status = status; w.ResponseWriter.WriteHeader(status) }

func Middleware(limiter *rate.Limiter, next http.Handler) http.Handler {
	prometheus.MustRegister(requests, external, outbox)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if limiter != nil && !limiter.Allow() {
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		started := time.Now()
		traceID := r.Header.Get("X-Trace-Id")
		if traceID == "" {
			traceID = uuid.NewString()
		}
		w.Header().Set("X-Trace-Id", traceID)
		wrapped := &writer{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(wrapped, r.WithContext(context.WithValue(r.Context(), TraceKey, traceID)))
		requests.WithLabelValues(r.Method, strconv.Itoa(wrapped.status)).Observe(time.Since(started).Seconds())
	})
}
func MetricsHandler() http.Handler { return promhttp.Handler() }
func ObserveExternalError(platform, operation string) {
	external.WithLabelValues(platform, operation).Inc()
}
func SetOutboxLag(seconds float64) { outbox.Set(seconds) }
