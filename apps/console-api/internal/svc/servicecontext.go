package svc

import (
	"os"
	"time"

	"github.com/example/customer-service-platform/apps/console-api/internal/config"
	"github.com/example/customer-service-platform/apps/console-api/internal/middleware"
	"github.com/example/customer-service-platform/pkg/dingtalk"
	"github.com/example/customer-service-platform/pkg/domain"
	"github.com/example/customer-service-platform/pkg/platform"
	"github.com/example/customer-service-platform/pkg/store"
	"github.com/example/customer-service-platform/pkg/wechat"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/rest"
)

type ServiceContext struct {
	Config        config.Config
	RequestLimits rest.Middleware
	Store         store.Store
	DingTalk      *dingtalk.Client
	WeChat        *wechat.Client
	Storage       domain.ObjectStorage
}

type Dependencies struct {
	Store    store.Store
	DingTalk *dingtalk.Client
	WeChat   *wechat.Client
	Storage  domain.ObjectStorage
}

func NewServiceContext(c config.Config) *ServiceContext {
	return NewServiceContextWithDependencies(c, nil)
}

func NewServiceContextWithDependencies(c config.Config, dependencies *Dependencies) *ServiceContext {
	if dependencies == nil {
		var err error
		dependencies, err = ProductionDependencies(c)
		if err != nil {
			panic(err)
		}
	}
	return &ServiceContext{Config: c, Store: dependencies.Store, DingTalk: dependencies.DingTalk, WeChat: dependencies.WeChat, Storage: dependencies.Storage, RequestLimits: middleware.NewRequestLimitsMiddleware().Handle}
}

func ProductionDependencies(c config.Config) (*Dependencies, error) {
	repository, err := store.OpenMySQL(platform.ConfigFromEnv().DSN)
	if err != nil {
		return nil, err
	}
	secrets := platform.EnvSecrets{}
	redisClient := redis.NewClient(&redis.Options{Addr: platform.ConfigFromEnv().RedisAddress, Password: platform.ConfigFromEnv().RedisPassword})
	storage, err := platform.NewStorage(platform.ConfigFromEnv().ObjectStorageEndpoint, "env://OBJECT_STORAGE_ACCESS_KEY", "env://OBJECT_STORAGE_SECRET_KEY", os.Getenv("OBJECT_STORAGE_BUCKET"), secrets)
	if err != nil {
		return nil, err
	}
	wechatClient, err := wechat.NewClient(wechat.Config{Store: repository, Secrets: secrets, Tokens: wechat.NewRedisTokenStore(redisClient), Timeout: 8 * time.Second})
	if err != nil {
		return nil, err
	}
	dingtalkClient, err := dingtalk.NewClient(dingtalk.Config{Secrets: secrets, Store: repository, AppKeySecretRef: "env://DINGTALK_APP_KEY", AppSecretRef: "env://DINGTALK_APP_SECRET", CardTemplateID: getenv("DINGTALK_CARD_TEMPLATE_ID"), CardSecretRef: "env://DINGTALK_CARD_API_SECRET"})
	if err != nil {
		return nil, err
	}
	return &Dependencies{Store: repository, DingTalk: dingtalkClient, WeChat: wechatClient, Storage: storage}, nil
}

func getenv(key string) string { return os.Getenv(key) }
