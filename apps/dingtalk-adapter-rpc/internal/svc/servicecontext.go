package svc

import (
	"os"
	"time"

	"github.com/example/customer-service-platform/apps/dingtalk-adapter-rpc/internal/config"
	"github.com/example/customer-service-platform/pkg/dingtalk"
	"github.com/example/customer-service-platform/pkg/platform"
	"github.com/example/customer-service-platform/pkg/store"
)

type ServiceContext struct {
	Config   config.Config
	Store    store.Store
	DingTalk *dingtalk.Client
}

func NewServiceContext(c config.Config) *ServiceContext {
	repository, err := store.OpenMySQL(os.Getenv("MYSQL_DSN"))
	if err != nil {
		panic(err)
	}
	client, err := productionClient(repository)
	if err != nil {
		panic(err)
	}
	return &ServiceContext{Config: c, Store: repository, DingTalk: client}
}

func NewServiceContextWithStore(c config.Config, repository store.Store) *ServiceContext {
	client, err := productionClient(repository)
	if err != nil {
		panic(err)
	}
	return &ServiceContext{Config: c, Store: repository, DingTalk: client}
}

func NewServiceContextWithClient(c config.Config, repository store.Store, client *dingtalk.Client) *ServiceContext {
	return &ServiceContext{Config: c, Store: repository, DingTalk: client}
}

func productionClient(repository store.Store) (*dingtalk.Client, error) {
	return dingtalk.NewClient(dingtalk.Config{
		Secrets: platform.EnvSecrets{}, Store: repository, Timeout: 8 * time.Second,
		AppKeySecretRef: "env://DINGTALK_APP_KEY", AppSecretRef: "env://DINGTALK_APP_SECRET",
		CardTemplateID: os.Getenv("DINGTALK_CARD_TEMPLATE_ID"), CardSecretRef: "env://DINGTALK_CARD_API_SECRET",
		APISecretRef: "env://DINGTALK_API_SECRET", CallbackRouteKey: os.Getenv("DINGTALK_CALLBACK_ROUTE_KEY"),
		AESKeySecretRef: "env://DINGTALK_CALLBACK_AES_KEY", CorpIDSecretRef: "env://DINGTALK_CORP_ID",
	})
}
