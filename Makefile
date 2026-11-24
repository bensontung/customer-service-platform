SHELL := /bin/sh

.PHONY: test test-unit test-integration lint vet generate generate-check frontend-test build up down migrate clean

GO_PACKAGES := ./...

test:
	go test $(GO_PACKAGES)
	npm --prefix web-console test

test-unit:
	go test $(GO_PACKAGES)

test-integration:
	REQUIRE_INTEGRATION=1 MYSQL_DSN="$${MYSQL_DSN:-$${TEST_MYSQL_DSN}}" go run ./deploy/migrate -dir deploy/migrations
	REQUIRE_INTEGRATION=1 go test ./pkg/store ./pkg/wechat ./pkg/mq ./pkg/platform

lint:
	golangci-lint run ./...
	npm --prefix web-console run typecheck

vet:
	go vet ./...

generate:
	goctl api go -api apps/console-api/console.api -dir apps/console-api
	goctl rpc protoc apps/conversation-rpc/conversation.proto --go_out=apps/conversation-rpc/pb --go-grpc_out=apps/conversation-rpc/pb --zrpc_out=apps/conversation-rpc
	goctl rpc protoc apps/customer-rpc/customer.proto --go_out=apps/customer-rpc/pb --go-grpc_out=apps/customer-rpc/pb --zrpc_out=apps/customer-rpc
	goctl rpc protoc apps/dingtalk-adapter-rpc/dingtalk_adapter.proto --go_out=apps/dingtalk-adapter-rpc/pb --go-grpc_out=apps/dingtalk-adapter-rpc/pb --zrpc_out=apps/dingtalk-adapter-rpc
	goctl rpc protoc apps/ticket-rpc/ticket.proto --go_out=apps/ticket-rpc/pb --go-grpc_out=apps/ticket-rpc/pb --zrpc_out=apps/ticket-rpc
	goctl rpc protoc apps/wechat-adapter-rpc/wechat_adapter.proto --go_out=apps/wechat-adapter-rpc/pb --go-grpc_out=apps/wechat-adapter-rpc/pb --zrpc_out=apps/wechat-adapter-rpc

generate-check:
	@set -eu; temp="$$(mktemp -d)"; trap 'rm -rf "$$temp"' EXIT; \
	cp -R apps "$$temp/apps"; cp go.mod "$$temp/go.mod"; \
	goctl api go -api "$$temp/apps/console-api/console.api" -dir "$$temp/apps/console-api"; \
	for spec in conversation-rpc/conversation customer-rpc/customer dingtalk-adapter-rpc/dingtalk_adapter ticket-rpc/ticket wechat-adapter-rpc/wechat_adapter; do \
		dir="$${spec%/*}"; file="$${spec##*/}"; (cd "$$temp/apps/$$dir" && goctl rpc protoc "$$file.proto" --go_out=pb --go-grpc_out=pb --zrpc_out=.); \
	done; \
	for generated in apps/console-api/internal/types/types.go apps/console-api/internal/handler/routes.go apps/conversation-rpc/pb/github.com/example/customer-service-platform/apps/conversation-rpc/pb/conversation.pb.go apps/customer-rpc/pb/github.com/example/customer-service-platform/apps/customer-rpc/pb/customer.pb.go apps/dingtalk-adapter-rpc/pb/github.com/example/customer-service-platform/apps/dingtalk-adapter-rpc/pb/dingtalk_adapter.pb.go apps/ticket-rpc/pb/github.com/example/customer-service-platform/apps/ticket-rpc/pb/ticket.pb.go apps/wechat-adapter-rpc/pb/github.com/example/customer-service-platform/apps/wechat-adapter-rpc/pb/wechat_adapter.pb.go; do \
		diff -u "$$generated" "$$temp/$$generated"; \
	done

frontend-test:
	npm --prefix web-console ci
	npm --prefix web-console test
	npm --prefix web-console run build

build:
	go build ./...
	npm --prefix web-console run build

migrate:
	go run ./deploy/migrate -dir deploy/migrations

up:
	docker compose -f deploy/docker-compose.yml up --build

down:
	docker compose -f deploy/docker-compose.yml down -v

clean:
	rm -rf web-console/dist coverage.out
