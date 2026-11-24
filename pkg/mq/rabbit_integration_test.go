package mq

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

func TestRabbitPublisherConfirmationAndRedelivery(t *testing.T) {
	address := os.Getenv("TEST_AMQP_URL")
	if address == "" {
		if os.Getenv("REQUIRE_INTEGRATION") == "1" {
			t.Fatal("TEST_AMQP_URL is required by REQUIRE_INTEGRATION=1")
		}
		t.Skip("set TEST_AMQP_URL to run isolated RabbitMQ integration tests")
	}
	queue := "integration-" + time.Now().UTC().Format("150405.000000000")
	publisher, err := OpenRabbit(address, queue)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = publisher.Close() }()
	consumer, err := OpenRabbit(address, queue)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = consumer.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err = publisher.Publish(ctx, "event-1", []byte(`{"type":"test"}`)); err != nil {
		t.Fatal(err)
	}
	var attempts atomic.Int32
	done := make(chan error, 1)
	go func() {
		done <- consumer.Consume(ctx, func(_ context.Context, eventID string, payload []byte) error {
			if eventID != "event-1" || string(payload) != `{"type":"test"}` {
				return fmt.Errorf("unexpected delivery %q %s", eventID, payload)
			}
			if attempts.Add(1) == 1 {
				return errors.New("force redelivery")
			}
			return nil
		})
	}()
	deadline := time.Now().Add(10 * time.Second)
	for attempts.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if attempts.Load() != 2 {
		t.Fatalf("delivery attempts = %d, want 2", attempts.Load())
	}
	cancel()
	if err = <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Consume() = %v, want context cancellation", err)
	}
}
