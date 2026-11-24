package main

import (
	"flag"
	"fmt"

	"github.com/example/customer-service-platform/apps/customer-rpc/internal/config"
	"github.com/example/customer-service-platform/apps/customer-rpc/internal/server"
	"github.com/example/customer-service-platform/apps/customer-rpc/internal/svc"
	"github.com/example/customer-service-platform/apps/customer-rpc/pb/github.com/example/customer-service-platform/apps/customer-rpc/pb"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var configFile = flag.String("f", "etc/customer.yaml", "the config file")

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c, conf.UseEnv())
	ctx := svc.NewServiceContext(c)

	s := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		pb.RegisterCustomerServiceServer(grpcServer, server.NewCustomerServiceServer(ctx))

		if c.Mode == service.DevMode || c.Mode == service.TestMode {
			reflection.Register(grpcServer)
		}
	})
	defer s.Stop()

	fmt.Printf("Starting rpc server at %s...\n", c.ListenOn)
	s.Start()
}
