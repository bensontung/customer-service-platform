FROM golang:1.24-alpine AS build
WORKDIR /src
RUN apk add --no-cache git
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/console ./apps/console-api && CGO_ENABLED=0 go build -trimpath -o /out/ingress ./apps/wechat-ingress-api && CGO_ENABLED=0 go build -trimpath -o /out/scheduler ./apps/scheduler-job && CGO_ENABLED=0 go build -trimpath -o /out/migrate ./deploy/migrate && for service in conversation customer ticket wechat-adapter dingtalk-adapter; do CGO_ENABLED=0 go build -trimpath -o "/out/${service}-rpc" "./apps/${service}-rpc"; done

FROM gcr.io/distroless/static-debian12:nonroot AS base
WORKDIR /app
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
FROM base AS console
COPY --from=build /out/console /app/server
COPY apps/console-api/etc/console-api.yaml /app/etc/console-api.yaml
ENTRYPOINT ["/app/server", "-f", "/app/etc/console-api.yaml"]
FROM base AS ingress
COPY --from=build /out/ingress /app/server
ENTRYPOINT ["/app/server"]
FROM base AS scheduler
COPY --from=build /out/scheduler /app/server
ENTRYPOINT ["/app/server"]
FROM base AS migrate
COPY --from=build /out/migrate /app/migrate
COPY deploy/migrations /migrations
ENTRYPOINT ["/app/migrate", "-dir", "/migrations"]
FROM base AS conversation-rpc
COPY --from=build /out/conversation-rpc /app/server
COPY apps/conversation-rpc/etc/conversation.yaml /app/etc/conversation.yaml
ENTRYPOINT ["/app/server", "-f", "/app/etc/conversation.yaml"]
FROM base AS customer-rpc
COPY --from=build /out/customer-rpc /app/server
COPY apps/customer-rpc/etc/customer.yaml /app/etc/customer.yaml
ENTRYPOINT ["/app/server", "-f", "/app/etc/customer.yaml"]
FROM base AS ticket-rpc
COPY --from=build /out/ticket-rpc /app/server
COPY apps/ticket-rpc/etc/ticket.yaml /app/etc/ticket.yaml
ENTRYPOINT ["/app/server", "-f", "/app/etc/ticket.yaml"]
FROM base AS wechat-adapter-rpc
COPY --from=build /out/wechat-adapter-rpc /app/server
COPY apps/wechat-adapter-rpc/etc/wechatadapter.yaml /app/etc/wechatadapter.yaml
ENTRYPOINT ["/app/server", "-f", "/app/etc/wechatadapter.yaml"]
FROM base AS dingtalk-adapter-rpc
COPY --from=build /out/dingtalk-adapter-rpc /app/server
COPY apps/dingtalk-adapter-rpc/etc/dingtalkadapter.yaml /app/etc/dingtalkadapter.yaml
ENTRYPOINT ["/app/server", "-f", "/app/etc/dingtalkadapter.yaml"]
