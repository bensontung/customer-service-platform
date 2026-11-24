package svc

import (
	"os"

	"github.com/example/customer-service-platform/apps/customer-rpc/internal/config"
	"github.com/example/customer-service-platform/pkg/store"
)

type ServiceContext struct {
	Config config.Config
	Store  store.Store
}

func NewServiceContext(c config.Config) *ServiceContext {
	repository, err := store.OpenMySQL(os.Getenv("MYSQL_DSN"))
	if err != nil {
		panic(err)
	}
	return NewServiceContextWithStore(c, repository)
}

func NewServiceContextWithStore(c config.Config, repository store.Store) *ServiceContext {
	return &ServiceContext{Config: c, Store: repository}
}
