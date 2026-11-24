# 微信与钉钉客服中台

Go 1.24（兼容 Go 1.23+ 规范）/ go-zero 后端，React + TypeScript 工作台，MySQL、Redis、RabbitMQ、MinIO 基础设施。实现规格见 `docs/wechat-dingtalk-customer-service-implementation-spec.md`。

## 当前状态

已实现并通过本地单元、HTTP Mock 端到端测试：

- 微信明文/密文回调验签、AES 解密、AppID 校验、幂等入库、快速 ACK；持久化失败不返回成功 ACK。
- 会话、领取/转派、48 小时用户发消息回复窗口及 5 条配额、文本/图片/语音回复、重试和最终失败状态。
- 钉钉 H5 免登、员工映射、互动卡片通知和签名回调领取。
- 文章评论独立同步与官方评论操作，CRM、工单、排班、基础路由、SLA 预警/超时、审计和基础报表。
- MySQL/Memory 仓储、事务 Outbox、可恢复 Outbox/消息发送租约、RabbitMQ 发布确认、Redis Token 缓存、MinIO 预签名上传。
- React 工作台 JWT 仅保存在内存，消息通过轮询确认最终状态。

尚未完成生产验收：真实微信/钉钉账号与权限、5 秒进线、实际送达、H5/浏览器完整登录、媒体真实上传、路由规则完整管理、可配置首响/后续响应 SLA、完整工单升级、补偿任务和运营报表。P3/P4 不能视为完成。

## 部署

Alibaba Cloud Linux 3 单机生产部署、HTTPS、初始化、渠道验收与运维步骤见 `docs/alibaba-cloud-linux-deployment.md`。

## 启动

前置：Go 1.24、Node.js 20、Docker Compose。

```sh
cp .env.example .env
# 在 .env 或部署 Secret 中设置至少 32 字节的 SESSION_SECRET；真实接入时再设置渠道 Secret。
set -a; . ./.env; set +a
make up
```

Web 工作台为 `http://localhost:3000`，Console BFF 为 `http://localhost:8080`。微信回调需要单独的公网 HTTPS 入口与渠道账号初始化；本地 Compose 不提供公网隧道、测试账号或 Secret。

数据库迁移也可独立执行：

```sh
export MYSQL_DSN='app:local-development-only@tcp(localhost:3306)/customer_service?charset=utf8mb4&parseTime=true&loc=UTC'
make migrate
```

迁移器按文件名顺序执行，并在同一连接上获取迁移锁、启用多语句 SQL。生产执行前必须备份数据库，并在预发布环境验证升级和回滚方案；当前迁移只提供向前脚本。

## Kubernetes 部署

`deploy/k8s` 提供 Kustomize 基础清单、五个 RPC、迁移 Job、PDB 和 NetworkPolicy。部署前必须由平台替换 `replace-me` 镜像、外部服务地址和 Ingress；`customer-service-secrets` 由 Secret Manager/ExternalSecrets 创建，至少包含 `MYSQL_DSN`、`AMQP_URL`、`SESSION_SECRET` 及对象存储、微信、钉钉所需 Secret 引用。

当前 Console 业务 Handler 直接使用共享仓储，五个 RPC 服务可独立部署但尚未切换为 Console 的必需远程依赖；不能据此宣称服务间 RPC 调用已完成端到端验收。

## 验证

```sh
make test-unit
make vet
make lint
make frontend-test
make generate-check
```

`make test-unit` 在未设置真实依赖变量时会明确跳过基础设施测试。强制运行 MySQL、Redis、RabbitMQ、MinIO 集成测试：

```sh
export REQUIRE_INTEGRATION=1
export TEST_MYSQL_DSN='app:local-development-only@tcp(localhost:3306)/customer_service?charset=utf8mb4&parseTime=true&loc=UTC'
export TEST_REDIS_ADDR=localhost:6379
export TEST_AMQP_URL='amqp://app:local-development-only@localhost:5672/'
export TEST_OBJECT_STORAGE_ENDPOINT=http://localhost:9000
export TEST_OBJECT_STORAGE_ACCESS_KEY=local-development
export TEST_OBJECT_STORAGE_SECRET_KEY=local-development-only
make test-integration
```

`make test-integration` 会先运行版本化迁移；直接运行 Go 集成测试时需先执行迁移，测试仅校验迁移版本，不会重放 SQL。

测试使用唯一事件、队列和 Bucket 名称；MySQL 集成测试要求专用空数据库，禁止指向开发或生产数据。CI 为基础设施测试创建独立服务并运行两次迁移。

## Secret 与渠道约束

- 所有微信/钉钉密钥仅从 `env://` 引用或部署 Secret Manager 注入。`.env` 不得提交，不得写入前端或日志。
- 微信安全模式使用 `msg_signature`，解密后验证 AppID。用户发消息场景的回复窗口是 48 小时/5 条；其他触发场景规则不同，不得统一宣称同一额度。
- 文章评论不是私信会话，不会转换为可私信回复的客服会话。
- 钉钉 H5 使用 `requestAuthCode`；新 API Token 使用 `x-acs-dingtalk-access-token`。浏览器 OAuth 尚未完成可用性验收。
- 不包含 AI 自动回复、AI 建议、RAG 或大模型能力。
