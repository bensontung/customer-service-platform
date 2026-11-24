# 微信服务号/小程序 → 钉钉客服中台实施规格

> **交付对象：** OpenCode / 开发团队  
> **技术基线：** Go 1.23+、go-zero、React、MySQL 8、Redis、RabbitMQ（可替换 RocketMQ/Kafka）  
> **版本：** v1.0  
> **范围状态：** MVP + CRM/工单/排班；**明确不实现 AI 自动回复、AI 建议回复或 RAG**。

---

## 1. 目标和边界

### 1.1 目标

建设一个客服中台，使客户在**微信服务号**和**微信小程序**发起的客服消息，以及在**服务号文章**发表的留言/评论，能被系统统一接入、分配和留痕。

客服人员：

1. 收到钉钉通知和待办；
2. 点击后在钉钉内嵌 H5 客服工作台处理；
3. 也可在普通浏览器使用同一 Web 工作台；
4. 在合法的微信消息窗口内回复客户；
5. 查询和维护客户资料、创建/处理工单、遵循排班与 SLA。

### 1.2 已确认条件

- 服务号和小程序属于同一主体，且拥有相应的客服接口权限。
- 评论来源为**服务号文章留言/评论**。
- 钉钉是内部客服入口，不以钉钉群聊文本作为主要回复界面。

### 1.3 非目标

- 不实现大模型、智能推荐、自动回复、知识库/RAG。
- 不替代微信原生管理后台的全部能力。
- 不处理视频号评论（后续独立评估）。
- 不假设文章评论可绕过微信权限或审核规则直接回私信。

### 1.4 关键渠道限制

- 服务号客服回复须遵守微信规定的客服消息窗口、条数及触发条件。当前产品规则配置为：用户消息后**48 小时内、最多 5 条**；实际发送前仍须以微信 API 返回结果为准。
- 服务号文章评论、服务号私信、小程序客服消息为不同来源，必须在 UI 和数据模型中明确区分。
- 所有微信、钉钉密钥仅可经部署环境的 Secret Manager / K8s Secret 注入；不得提交到 Git、前端或日志。

---

## 2. 总体架构

```text
微信服务号 / 微信小程序 / 服务号文章评论
               │ HTTPS 回调
               ▼
     wechat-ingress-api（验签、解密、去重、快速 ACK）
               │ Outbox / MQ
               ▼
   conversation-rpc ─── customer-rpc ─── ticket-rpc
       │                     │                │
       ├── wechat-adapter-rpc │                └── SLA / 排班任务
       └── dingtalk-adapter-rpc
               │
               ├── 钉钉通知 / 互动卡片 / 待办
               └── 内嵌 H5 与浏览器 Web 工作台

           MySQL + Redis + MQ + 对象存储
```

### 2.1 实施原则

1. **先模块化单体，后按负载拆服务。** 首版可置于一个 go-zero 仓库、多个 `api/rpc/mq` 进程；不必一开始部署大量独立服务。
2. **回调短路径。** 微信回调仅执行验签、解密、幂等写入和入队，目标 P95 小于 500ms；耗时工作由消费者完成。
3. **至少一次投递 + 幂等消费。** 外部平台回调可能重试，内部 MQ 也可能重复投递。
4. **渠道适配隔离。** 前端和会话领域层不得直接依赖微信或钉钉 SDK/HTTP DTO。

---

## 3. 仓库结构

```text
customer-service-platform/
├── apps/
│   ├── wechat-ingress-api/       # 微信回调入口
│   ├── console-api/              # Web/钉钉 H5 的 BFF API
│   ├── conversation-rpc/
│   ├── customer-rpc/
│   ├── ticket-rpc/
│   ├── wechat-adapter-rpc/
│   ├── dingtalk-adapter-rpc/
│   └── scheduler-job/            # 排班、SLA、补偿任务
├── pkg/
│   ├── domain/                   # DTO、枚举、领域错误
│   ├── wechat/                   # 仅含可复用协议、签名与加解密
│   ├── dingtalk/
│   ├── mq/
│   ├── idempotency/
│   └── observability/
├── deploy/
│   ├── docker-compose.yml
│   ├── k8s/
│   └── migrations/
├── web-console/                  # React + TypeScript + Vite
├── docs/
└── Makefile
```

要求：使用 `goctl` 管理 go-zero 的 `.api` / `.proto` 生成物；生成代码不得手工编辑。所有跨服务契约都必须版本化。

---

## 4. 领域模型与数据库

使用 MySQL 8、`utf8mb4`、UTC 时间存储；主键采用 UUIDv7 或雪花 ID。以下是最小表集。

| 表 | 关键字段 | 用途 |
|---|---|---|
| `channel_account` | `id, channel, app_id, status` | 服务号、小程序账户配置元数据（不存明文密钥） |
| `external_customer` | `id, channel, app_id, openid, unionid, nickname` | 外部客户身份；`channel+app_id+openid` 唯一 |
| `conversation` | `id, source, customer_id, status, assignee_id, last_inbound_at, reply_deadline_at` | 私信/客服会话 |
| `message` | `id, conversation_id, direction, channel_msg_id, msg_type, content_json, status` | 统一消息流水；`channel+channel_msg_id` 唯一 |
| `article_comment` | `id, channel_account_id, msg_data_id, comment_id, author_id, content, status` | 服务号文章评论/留言 |
| `agent` | `id, dingtalk_user_id, name, role, active` | 客服人员及钉钉身份映射 |
| `team` / `team_agent` | 组与成员 | 技能组、路由组 |
| `shift` | `agent_id/team_id, start_at, end_at, state` | 排班记录 |
| `routing_rule` | `priority, match_json, target_team_id` | 渠道/标签/时段路由规则 |
| `ticket` | `id, customer_id, conversation_id, status, priority, assignee_id, due_at` | 工单 |
| `ticket_event` | `ticket_id, event_type, payload_json` | 工单审计流水 |
| `outbox_event` | `id, event_type, aggregate_id, payload_json, published_at` | 可靠地向 MQ 投递事件 |
| `inbox_dedup` | `source, event_id, received_at` | 回调与消费幂等 |
| `audit_log` | `actor_id, action, resource_type, resource_id, detail_json` | 操作审计 |

**索引要求：**

- `message(conversation_id, created_at)`；
- `conversation(assignee_id, status, last_inbound_at)`；
- `article_comment(channel_account_id, comment_id)` 唯一；
- `ticket(assignee_id, status, due_at)`；
- 所有外部事件 ID 具备唯一索引。

`content_json` 仅保存结构化内容与对象存储引用；媒体原文件不可直接存入数据库。

---

## 5. 接入与事件流

### 5.1 微信入站消息

1. 微信请求 `POST /callbacks/wechat/{accountKey}`。
2. 校验 URL / 回调签名，按账户配置完成必要的消息解密。
3. 将原始请求的哈希、外部消息 ID 写入 `inbox_dedup`；重复请求直接返回成功。
4. 在同一事务写入规范化入站消息与 `outbox_event(MessageReceived)`。
5. 立即向微信返回成功。
6. MQ 消费者创建/更新客户、会话，计算 `reply_deadline_at`，执行路由，推送钉钉通知。

### 5.2 服务号文章评论

以微信当前开放能力实现“获取/管理评论”的适配器。要求：

- 定时同步应使用游标/时间窗口且幂等；若微信可提供事件回调则优先使用事件回调。
- 评论进入 `article_comment`，而非伪造为 `conversation`；客服可审核、回复、精选、删除（只展示当前授权接口允许的操作）。
- 可从评论页面“一键创建工单”；不得默认开启私信会话或承诺可私信回复。

### 5.3 客服回复

1. 客服在工作台发送消息。
2. `console-api` 验证当前客服是会话责任人或有接管权限。
3. `conversation-rpc` 检查会话状态及 `reply_deadline_at`；不满足条件则返回明确业务错误，不进入发送队列。
4. 写入 `message(direction=outbound,status=queued)` 和 `outbox_event(MessageSendRequested)`。
5. `wechat-adapter-rpc` 按渠道上传媒体（如有）、发送客服消息、记录平台响应。
6. 成功置为 `sent`；可重试故障置为 `retrying`；窗口过期、参数不合法等置为 `failed_final`，并在 UI 提示原因。

### 5.4 钉钉通知和内嵌工作台

- 新会话、转派、即将超时、工单升级时，给指定客服或团队发送钉钉互动卡片/工作通知。
- 卡片仅放摘要和操作按钮（领取、查看、转派），跳转至：`https://console.example.com/dingtalk/...`。
- H5 使用钉钉 JSAPI 获取免登临时授权信息；后端校验钉钉身份并签发短期本系统 JWT/Session。
- 浏览器版使用企业 SSO（优先钉钉 OAuth）或管理员配置的账号体系。禁止前端伪造 `agentId`。

---

## 6. API 契约（首版）

所有业务接口前缀：`/api/v1`。接口由 go-zero `.api` 文件定义并生成基础代码。

| 方法与路径 | 权限 | 说明 |
|---|---|---|
| `GET /me` | 已登录 | 当前客服、角色、排班状态 |
| `GET /conversations` | 客服/主管 | 队列与我的会话；支持 `status/source/assignee` 筛选 |
| `GET /conversations/:id` | 会话权限 | 会话、客户、消息、关联工单 |
| `POST /conversations/:id/claim` | 客服 | 领取等待会话（乐观锁/条件更新） |
| `POST /conversations/:id/transfer` | 客服/主管 | 转交人员或团队，须写审计 |
| `POST /conversations/:id/messages` | 会话权限 | 发送文本/图片/语音；异步返回 `messageId` |
| `POST /conversations/:id/close` | 会话权限 | 关闭会话 |
| `GET /comments` | 客服/主管 | 服务号文章评论列表 |
| `POST /comments/:id/actions` | 授权客服 | `approve/reply/feature/delete`；后端按微信实际权限校验 |
| `GET /customers/:id` | 客服/主管 | 客户资料、标签、会话、工单 |
| `PATCH /customers/:id` | 客服/主管 | 标签、备注等 CRM 基础信息 |
| `POST /tickets` | 客服 | 从客户/会话/评论创建工单 |
| `PATCH /tickets/:id` | 工单权限 | 更新状态、优先级、负责人、截止时间 |
| `GET /admin/shifts` | 管理员 | 排班查询 |
| `POST /admin/shifts` | 管理员 | 创建/更新排班 |
| `POST /callbacks/wechat/:accountKey` | 外部微信 | 微信回调，无用户 JWT |
| `POST /callbacks/dingtalk/*` | 外部钉钉 | 钉钉事件/卡片回调，验签后处理 |

发送消息的最小请求示例：

```json
{
  "type": "text",
  "text": "您好，已收到您的反馈，我们正在为您处理。"
}
```

上传文件走预签名上传：`POST /media/upload-credentials`，再以对象键/媒体 ID 引用发送；不得将大文件经 API JSON Base64 上传。

---

## 7. 前端工作台

### 7.1 页面

1. **登录/免登页**：识别钉钉内嵌环境并完成免登。
2. **会话队列**：我的会话、待领取、团队队列；未读数、来源、最后消息、SLA 倒计时。
3. **会话详情**：左侧队列、中间消息流、右侧客户资料/工单；明显展示来源及“可回复至 xx:xx”。
4. **评论管理**：文章、评论人、内容、审核状态、操作历史；与私信界面分开。
5. **工单中心**：列表、详情、流转时间线。
6. **排班与路由管理**：仅管理员可见。
7. **审计/报表（基础）**：新会话数、首响时长、超时数、关闭数；不涉及 AI 指标。

### 7.2 UX 规则

- 会话未领取时回复框禁用，仅显示“领取”。
- 回复窗口过期时回复框禁用，显示平台限制原因。
- 发送使用本地 pending 状态；API 成功不等于微信送达，轮询/WebSocket/SSE 更新最终状态。
- 图片、语音、文件都必须显示上传/转码/发送失败状态并可重试。

---

## 8. 路由、排班、SLA

首版路由顺序：

1. 过滤当前在班且 `active=true` 的客服；
2. 应用渠道、账户、客户标签、时间段的最高优先级规则；
3. 选择目标团队中当前进行中会话数最少的客服；
4. 没有在班客服时进入团队等待队列，并通知值班主管；
5. 客服可手动转派；主管可强制接管。

可配置 SLA：首次响应时限、后续响应时限、工单处理截止时间。任务每分钟扫描即将超时与已超时对象；事件必须幂等，避免重复提醒。

---

## 9. 安全、合规与运维

- 微信回调：严格签名验证、时间戳/nonce 校验、必要时 AES 解密；原始 payload 仅脱敏短期保存。
- 钉钉回调：按钉钉最新回调规范验签与解密；拒绝未知事件类型。
- 认证授权：RBAC 至少含 `agent`、`supervisor`、`admin`；后端逐资源鉴权。
- 敏感数据：手机号等加密或脱敏展示；审计日志禁止记录 access token、secret、完整隐私内容。
- 限流：回调、工作台 API、对外发送 API 分别限流；发送端按渠道 API 限额实现退避。
- 可观测性：每个外部事件贯穿 `trace_id`；日志结构化；监控回调失败率、MQ 积压、微信发送失败率、SLA 超时、Token 刷新异常。
- 备份：数据库每日全备 + 时间点恢复策略；对象存储版本与生命周期策略。

---

## 10. 开发任务与验收标准

### P0：工程与安全基座

- [ ] 初始化 go-zero monorepo、Makefile、lint、单测、Docker Compose、CI。
- [ ] 创建 MySQL migration、Redis、MQ、对象存储本地模拟环境。
- [ ] 建立配置 schema；所有密钥采用环境 Secret 引用。
- [ ] 接入 trace、日志、health/readiness metrics。

**验收：** 空环境用一条命令启动；`make test`、`make lint` 通过；任何日志和示例配置均无真实密钥。

### P1：微信消息与会话 MVP

- [ ] 服务号/小程序回调验签、解密、去重、入库、异步化。
- [ ] 会话与消息领域服务、工作台会话列表/详情。
- [ ] 微信客服文本消息回发、Token 缓存与刷新、失败重试。
- [ ] 可回复窗口提示与强制拦截。

**验收：** 用微信沙箱/测试账户发送一条消息，工作台 5 秒内出现；客服回复后能送回正确用户；重复回调不会产生重复消息或重复通知。

### P2：钉钉入口

- [ ] 钉钉内部应用、免登、员工身份映射。
- [ ] 新会话/转派/SLA 钉钉通知与互动卡片。
- [ ] 钉钉内嵌 H5 打开并可领取、查看、回复会话。

**验收：** 指定钉钉客服收到通知、可打开会话、完成回复；无权限员工不能访问该会话。

### P3：文章评论、CRM 与工单

- [ ] 服务号文章评论同步、去重、列表与授权操作。
- [ ] 客户标签/备注、历史会话查询。
- [ ] 工单创建、指派、状态流转、关联会话/评论。

**验收：** 指定文章评论仅入评论队列；可按微信授权范围处理；可从评论创建可追溯工单。

### P4：排班、路由、SLA 与运营

- [ ] 排班管理、按团队/负载路由。
- [ ] SLA 扫描、告警、升级。
- [ ] 基础报表、审计查询、错误补偿后台任务。

**验收：** 非排班客服不被自动分配；模拟超时只升级一次；审计可还原领取、回复、转派和工单状态变更。

---

## 11. 测试要求

- 单元测试：签名验证、加解密、窗口判断、幂等键、路由、SLA、权限判断，核心领域覆盖率不低于 80%。
- 集成测试：MySQL/Redis/MQ 使用 Testcontainers 或等价隔离环境。
- 契约测试：微信、钉钉 HTTP client 使用录制 fixture / mock server，覆盖成功、限流、超时、重复回调、Token 失效。
- 端到端测试：从模拟回调到钉钉通知、H5 领取、发送回微信的完整链路。
- 压测：验证回调入口能在外部依赖不可用时快速 ACK 且队列可恢复消费。

---

## 12. 开发前必须补齐的配置（不得写入代码仓库）

1. 服务号 AppID 与服务器回调配置；对应密钥由运维通过密钥管理系统配置。
2. 小程序 AppID 与客服消息回调配置。
3. 服务号文章评论能力、文章范围及运营审核规则。
4. 钉钉企业内部应用、回调地址、互动卡片模板和所需权限审批。
5. 域名、HTTPS 证书、对象存储桶、数据库/Redis/MQ 环境。
6. 首版客服团队、钉钉用户映射、角色、排班、路由及 SLA 规则。

---

## 13. 官方文档核验入口

- 微信服务号客服消息介绍：<https://developers.weixin.qq.com/doc/service/guide/product/kf/intro.html>
- 微信发送客服消息：<https://developers.weixin.qq.com/doc/service/api/customer/message/api_sendcustommessage>
- 钉钉互动卡片：<https://open.dingtalk.com/document/org/flow-type-interactive-card>
- 钉钉发送互动卡片：<https://open.dingtalk.com/document/robots/send-interactive-dynamic-cards>

在实现前，开发者必须逐项核验微信服务号文章评论相关 API、服务号/小程序客服消息 API 及钉钉回调/免登 API 的**最新权限要求、接口路径、频率限制与错误码**；本文不以旧版接口细节替代官方文档。
