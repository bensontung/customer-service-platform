package main

import (
	"flag"
	"fmt"

	"github.com/example/customer-service-platform/apps/console-api/internal/bootstrap"
	"github.com/example/customer-service-platform/apps/console-api/internal/config"
	"github.com/example/customer-service-platform/apps/console-api/internal/handler"
	"github.com/example/customer-service-platform/apps/console-api/internal/svc"
	"github.com/example/customer-service-platform/pkg/platform"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/rest"
)

var configFile = flag.String("f", "etc/console-api.yaml", "the config file")

func main() {
	bootstrap.ConfigureErrorHandler()
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c, conf.UseEnv())
	if len(c.Auth.AccessSecret) < 32 {
		panic("SESSION_SECRET must be at least 32 bytes")
	}
	if c.Auth.AccessExpire > 1800 {
		panic("session lifetime must not exceed 1800 seconds")
	}
	c.MetricsUrl = "/api/v1/metrics"

	server := rest.MustNewServer(c.RestConf)
	defer server.Stop()

	ctx := svc.NewServiceContext(c)
	handler.RegisterHandlers(server, ctx)
	server.AddRoute(rest.Route{Method: "POST", Path: "/api/v1/callbacks/dingtalk/card", Handler: bootstrap.DingTalkCallback{SVC: ctx, Secrets: platform.EnvSecrets{}, SecretRef: "env://DINGTALK_CARD_API_SECRET"}.ServeHTTP})
	server.AddRoute(rest.Route{Method: "POST", Path: "/api/v1/callbacks/dingtalk/events", Handler: bootstrap.DingTalkEvent{SVC: ctx, Secrets: platform.EnvSecrets{}, TokenRef: "env://DINGTALK_CALLBACK_TOKEN", AESKeyRef: "env://DINGTALK_CALLBACK_AES_KEY", CorpIDRef: "env://DINGTALK_CORP_ID"}.ServeHTTP})

	fmt.Printf("Starting server at %s:%d...\n", c.Host, c.Port)
	server.Start()
}
