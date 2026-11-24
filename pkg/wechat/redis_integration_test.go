package wechat

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestRedisTokenStoreLifecycle(t *testing.T) {
	address := os.Getenv("TEST_REDIS_ADDR")
	if address == "" {
		if os.Getenv("REQUIRE_INTEGRATION") == "1" {
			t.Fatal("TEST_REDIS_ADDR is required by REQUIRE_INTEGRATION=1")
		}
		t.Skip("set TEST_REDIS_ADDR to run isolated Redis integration tests")
	}
	client := redis.NewClient(&redis.Options{Addr: address})
	defer func() { _ = client.Close() }()
	tokens := NewRedisTokenStore(client)
	ctx := context.Background()
	key := "integration-token-" + time.Now().UTC().Format("150405.000000000")
	if err := tokens.Set(ctx, key, "cached-token", time.Minute); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tokens.Delete(context.Background(), key) }()
	value, err := tokens.Get(ctx, key)
	if err != nil || value != "cached-token" {
		t.Fatalf("Get() = %q, %v", value, err)
	}
	if err = tokens.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	value, err = tokens.Get(ctx, key)
	if err != nil || value != "" {
		t.Fatalf("deleted Get() = %q, %v", value, err)
	}
}
