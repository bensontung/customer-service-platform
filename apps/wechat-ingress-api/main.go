package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/example/customer-service-platform/pkg/observability"
	"github.com/example/customer-service-platform/pkg/platform"
	"github.com/example/customer-service-platform/pkg/store"
	"github.com/example/customer-service-platform/pkg/wechat/callback"
	"golang.org/x/time/rate"
)

func main() {
	repository, err := store.OpenMySQL(platform.ConfigFromEnv().DSN)
	if err != nil {
		panic(err)
	}
	defer func() { _ = repository.Close() }()
	callbacks := callback.NewHandler(repository)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /callbacks/wechat/{accountKey}", callbacks.Verify)
	mux.HandleFunc("POST /callbacks/wechat/{accountKey}", callbacks.Receive)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	mux.Handle("/metrics", observability.MetricsHandler())
	address := os.Getenv("WECHAT_INGRESS_LISTEN_ON")
	if address == "" {
		address = ":8081"
	}
	server := &http.Server{Addr: address, Handler: observability.Middleware(rate.NewLimiter(100, 200), mux), ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 2 * time.Second, IdleTimeout: 60 * time.Second}
	go func() {
		if err = server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			panic(err)
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
}
