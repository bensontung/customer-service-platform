package platform

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/example/customer-service-platform/pkg/domain"
)

type EnvSecrets struct{}

func (EnvSecrets) Value(_ context.Context, reference string) (string, error) {
	if reference == "" {
		return "", nil
	}
	if !strings.HasPrefix(reference, "env://") {
		return "", fmt.Errorf("secret reference must use env://")
	}
	key := strings.TrimPrefix(reference, "env://")
	if key != strings.ToUpper(key) || strings.ContainsAny(key, " /;\\") {
		return "", fmt.Errorf("invalid secret environment reference")
	}
	value, ok := os.LookupEnv(key)
	if !ok || value == "" {
		return "", fmt.Errorf("required secret %q is not configured", key)
	}
	return value, nil
}

type Config struct {
	DSN, RedisAddress, RedisPassword, AMQPURL string
	ConsoleURL, ObjectStorageEndpoint         string
	Environment                               string
}

func ConfigFromEnv() Config {
	return Config{
		DSN: os.Getenv("MYSQL_DSN"), RedisAddress: os.Getenv("REDIS_ADDR"),
		RedisPassword: os.Getenv("REDIS_PASSWORD"), AMQPURL: os.Getenv("AMQP_URL"),
		ConsoleURL: os.Getenv("CONSOLE_BASE_URL"), ObjectStorageEndpoint: os.Getenv("OBJECT_STORAGE_ENDPOINT"),
		Environment: os.Getenv("APP_ENV"),
	}
}

var _ domain.Secrets = EnvSecrets{}
