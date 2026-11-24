package svc

import (
	"os"
	"strings"
	"time"

	"github.com/example/customer-service-platform/apps/wechat-adapter-rpc/internal/config"
	"github.com/example/customer-service-platform/pkg/platform"
	"github.com/example/customer-service-platform/pkg/store"
	"github.com/example/customer-service-platform/pkg/wechat"
	"github.com/redis/go-redis/v9"
)

type ServiceContext struct {
	Config config.Config
	Store  store.Store
	WeChat *wechat.Client
}

func NewServiceContext(c config.Config) *ServiceContext {
	repository, err := store.OpenMySQL(os.Getenv("MYSQL_DSN"))
	if err != nil {
		panic(err)
	}
	var tokens wechat.TokenStore
	if address := strings.TrimSpace(os.Getenv("REDIS_ADDR")); address != "" {
		client := redis.NewClient(&redis.Options{Addr: address, Password: os.Getenv("REDIS_PASSWORD"), DB: 0})
		tokens = wechat.NewRedisTokenStore(client)
	} else if !localMode(os.Getenv("APP_ENV")) {
		panic("REDIS_ADDR is required outside local mode")
	}
	client, err := wechat.NewClient(wechat.Config{Store: repository, Secrets: platform.EnvSecrets{}, Tokens: tokens, Timeout: 8 * time.Second})
	if err != nil {
		panic(err)
	}
	return &ServiceContext{Config: c, Store: repository, WeChat: client}
}

func NewServiceContextWithStore(c config.Config, repository store.Store) *ServiceContext {
	client, err := wechat.NewClient(wechat.Config{Store: repository, Secrets: platform.EnvSecrets{}, Timeout: 8 * time.Second})
	if err != nil {
		panic(err)
	}
	return &ServiceContext{Config: c, Store: repository, WeChat: client}
}

func NewServiceContextWithClient(c config.Config, repository store.Store, client *wechat.Client) *ServiceContext {
	return &ServiceContext{Config: c, Store: repository, WeChat: client}
}

func localMode(environment string) bool {
	switch strings.ToLower(strings.TrimSpace(environment)) {
	case "", "local", "dev", "development", "test":
		return true
	default:
		return false
	}
}
