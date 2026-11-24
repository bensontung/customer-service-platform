package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	worker "github.com/example/customer-service-platform/pkg/conversation"
	"github.com/example/customer-service-platform/pkg/dingtalk"
	"github.com/example/customer-service-platform/pkg/domain"
	"github.com/example/customer-service-platform/pkg/mq"
	"github.com/example/customer-service-platform/pkg/observability"
	"github.com/example/customer-service-platform/pkg/platform"
	"github.com/example/customer-service-platform/pkg/store"
	"github.com/example/customer-service-platform/pkg/wechat"
	"github.com/redis/go-redis/v9"
)

func main() {
	config := platform.ConfigFromEnv()
	repository, err := store.OpenMySQL(config.DSN)
	if err != nil {
		panic(err)
	}
	defer func() { _ = repository.Close() }()
	secrets := platform.EnvSecrets{}
	redisClient := redis.NewClient(&redis.Options{Addr: config.RedisAddress, Password: config.RedisPassword})
	wechatClient, err := wechat.NewClient(wechat.Config{Store: repository, Secrets: secrets, Tokens: wechat.NewRedisTokenStore(redisClient), Timeout: 8 * time.Second})
	if err != nil {
		panic(err)
	}
	storage, err := platform.NewStorage(config.ObjectStorageEndpoint, "env://OBJECT_STORAGE_ACCESS_KEY", "env://OBJECT_STORAGE_SECRET_KEY", os.Getenv("OBJECT_STORAGE_BUCKET"), secrets)
	if err != nil {
		panic(err)
	}
	mediaSender := wechat.MediaSender{Client: wechatClient, Storage: storage}
	dingtalkClient, err := dingtalk.NewClient(dingtalk.Config{Secrets: secrets, Store: repository, AppKeySecretRef: "env://DINGTALK_APP_KEY", AppSecretRef: "env://DINGTALK_APP_SECRET", CardTemplateID: os.Getenv("DINGTALK_CARD_TEMPLATE_ID"), CallbackRouteKey: os.Getenv("DINGTALK_CALLBACK_ROUTE_KEY"), CardSecretRef: "env://DINGTALK_CARD_API_SECRET"})
	if err != nil {
		panic(err)
	}
	publisher, err := mq.OpenRabbit(config.AMQPURL, "customer_service.events")
	if err != nil {
		panic(err)
	}
	defer func() { _ = publisher.Close() }()
	consumer, err := mq.OpenRabbit(config.AMQPURL, "customer_service.events")
	if err != nil {
		panic(err)
	}
	defer func() { _ = consumer.Close() }()
	processor := &worker.Worker{Store: repository, Sender: mediaSender, Notifier: dingtalkClient, ConsoleURL: config.ConsoleURL}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		_ = consumer.Consume(ctx, func(ctx context.Context, _ string, payload []byte) error {
			var event domainEvent
			if err := json.Unmarshal(payload, &event); err != nil {
				return err
			}
			return processor.Process(ctx, event.toDomain())
		})
	}()
	go processor.RunCommentSync(ctx, wechatClient, worker.CommentSources())
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			_ = processor.SLA(ctx)
			_ = processor.Retry(ctx)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	mux := http.NewServeMux()
	mux.Handle("/metrics", observability.MetricsHandler())
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	server := &http.Server{Addr: ":8082", Handler: mux, ReadHeaderTimeout: 3 * time.Second}
	go func() { _ = server.ListenAndServe() }()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		events, pollErr := repository.ClaimOutbox(ctx, time.Now().UTC())
		if pollErr != nil {
			panic(pollErr)
		}
		for _, event := range events {
			payload, _ := json.Marshal(event)
			if err = publisher.Publish(ctx, event.ID, payload); err != nil {
				_ = repository.ReleaseOutbox(context.Background(), event.ID, time.Now().Add(2*time.Second))
				continue
			}
			if err = repository.MarkOutboxPublished(ctx, event.ID); err != nil {
				panic(err)
			}
		}
		select {
		case <-ctx.Done():
			_ = server.Shutdown(context.Background())
			return
		case <-ticker.C:
		}
	}
}

type domainEvent struct {
	ID          string          `json:"id"`
	Type        string          `json:"type"`
	AggregateID string          `json:"aggregateId"`
	Payload     json.RawMessage `json:"payload"`
	Attempts    int             `json:"attempts"`
}

func (e domainEvent) toDomain() domain.OutboxEvent {
	return domain.OutboxEvent{ID: e.ID, Type: e.Type, AggregateID: e.AggregateID, Payload: e.Payload, Attempts: e.Attempts}
}
