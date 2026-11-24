# Alibaba Cloud Linux 3 单机部署手册

适用系统：Alibaba Cloud Linux 3.2104 LTS 64 位。本文采用 Docker Compose + 宿主机 Nginx HTTPS 的单机部署方式，不需要安装 Go、Node.js 或 goctl。生产覆盖文件使用 Compose `!override` 标签，要求 Docker Compose v2.24.4 或更高版本；本文的生产配置使用 v2.40.3 进行离线解析校验。

> 本文用于部署当前代码，不代表已经完成真实微信/钉钉生产验收。部署后必须完成第 11 节的真实渠道测试。当前工作台仅支持钉钉内免登，普通浏览器 OAuth 尚未启用。
>
> 离线验证已完成：Go/前端单测、类型检查、lint、生成代码一致性、竞态测试、Compose v2.40.3 解析与回环端口断言。当前开发环境没有 Docker daemon 或 Nginx，因此尚未执行真实镜像构建、容器启动、Nginx `nginx -t`、MySQL 8.4/MinIO 集成及浏览器实际上传；这些必须在目标主机或 CI 中完成，不能视为已通过。

## 1. 部署结构与准备清单

请求路径：

```text
微信服务器  -> HTTPS /callbacks/wechat/{accountKey} -> wechat-ingress-api
钉钉平台    -> HTTPS /api/v1/callbacks/dingtalk/*    -> console-api
钉钉内工作台 -> HTTPS /                              -> Web -> Console API
客服发送消息 -> RabbitMQ consumer / scheduler          -> 微信/钉钉 API

MySQL：客户、会话、消息、工单、Outbox、幂等记录
Redis：微信 Access Token 缓存
RabbitMQ：可靠异步事件投递
MinIO：客服图片、语音等附件，私有 Bucket
```

宿主机 Nginx 处理 TLS；容器网关将 `/api/` 转发给 Console、其他页面转发给 Web。独立存储域名 `storage.example.com` 仅将 `/customer-service/` 对象路径代理到 MinIO，保留预签名请求的 URI 和 Host；微信入口独立，避免渠道回调混入 JWT 鉴权路由。

开始前准备：

| 项目 | 要求 |
| --- | --- |
| ECS | 建议至少 4 vCPU、8 GiB 内存、100 GiB SSD；生产规模按消息量和保留期评估 |
| 网络 | 出站可访问微信 `api.weixin.qq.com`、钉钉 `api.dingtalk.com` 和 `oapi.dingtalk.com` |
| 域名 | 已备案（如适用）、`console.example.com` 和 `storage.example.com` 的 DNS 均指向 ECS 公网 IP |
| HTTPS | 覆盖工作台和存储两个域名的有效证书（单张多域名或分别配置）；微信和钉钉回调使用有效公网 HTTPS 证书 |
| 微信 | 服务号/小程序 AppID、AppSecret、回调 Token、EncodingAESKey、接口权限及 IP 白名单配置 |
| 钉钉 | 企业内部应用、Client ID/AppKey、AppSecret、CorpID、所需 API 权限、互动卡片模板和回调配置 |
| 账号 | 客服钉钉 `userId`、角色、技能组、排班、路由初始数据 |
| 凭据 | 独立随机的 MySQL、Redis、RabbitMQ、MinIO、Session 和渠道密钥；不得复用示例值 |

不要开放公网入站的 `3306`、`6379`、`5672`、`9000`、`9001`、`15672`、`8080`、`8081`、`8082`、`8088`。生产仅开放 SSH、80、443；SSH 建议限制办公出口 IP，并优先通过阿里云安全组/堡垒机管理。

当前 Console 直接访问共享仓储。`deploy/k8s/` 中的五个 RPC 可独立构建，但本手册的 Compose 拓扑不启动这些非必需 RPC，不能据此宣称 RPC 服务端到端验收。

## 2. 主机初始化、时间同步与安全组

使用具有 sudo 权限的运维账号登录 ECS。本文命令默认在 `/opt/customer-service` 执行。

```bash
cat /etc/os-release
uname -m
sudo dnf update -y
sudo dnf install -y git nginx chrony policycoreutils-python-utils curl tar
sudo systemctl enable --now chronyd nginx
chronyc tracking
timedatectl set-timezone Asia/Shanghai
```

业务数据库使用 UTC 时间戳，宿主机时区可设为上海；微信/钉钉回调要求服务器时间准确，不能跳过 NTP 检查。

安全组建议：

| 方向 | 端口 | 来源/目标 |
| --- | --- | --- |
| 入站 | 22/tcp | 运维出口 IP 或堡垒机 |
| 入站 | 80/tcp | 公网，HTTP 跳转 HTTPS、证书续期 |
| 入站 | 443/tcp | 公网，工作台和渠道回调 |
| 出站 | 443/tcp | 微信、钉钉、镜像仓库、证书/备份服务 |
| 出站 | 53、123 | DNS、NTP，按主机网络策略配置 |

系统防火墙如果启用，允许 Web 服务即可，不需要为容器内部端口添加公网规则：

```bash
sudo systemctl status firewalld
# 仅在 firewalld 正在运行时执行：
sudo firewall-cmd --permanent --add-service=http --add-service=https
sudo firewall-cmd --reload
```

## 3. 安装 Docker 和 Compose 插件

Docker 官方仓库安装参考：`https://docs.docker.com/engine/install/centos/`。执行前核对该文档的最新包名与 RHEL/CentOS 兼容要求，不要直接照搬其他发行版的 APT/Ubuntu 命令。

优先使用 Alibaba Cloud Linux 软件源；不同镜像源的软件包名称可能不同，先查询再安装：

```bash
dnf list available 'docker*' 'containerd*' | less
```

如果源中提供 Docker 及 Compose 插件，安装对应包：

```bash
sudo dnf install -y docker docker-cli containerd docker-compose-plugin
```

若提示找不到包，不要混用不匹配的发行版二进制仓库。请按 Docker 官方适用于 CentOS/RHEL 兼容系统的仓库指引，为 Alibaba Cloud Linux 3 配置兼容的 `docker-ce`、`docker-ce-cli`、`containerd.io`、`docker-buildx-plugin`、`docker-compose-plugin`，或采用企业已有容器运行时。安装后验证：

```bash
sudo systemctl enable --now docker
sudo docker version
sudo docker compose version
sudo usermod -aG docker "$USER"
```

确认 `docker compose version` 不低于 v2.24.4，否则升级 Compose 插件后再继续。

退出 SSH 后重新登录，使 docker 用户组生效。本文后续省略 `sudo docker` 的 sudo；若当前账号未加入 docker 组，请给所有 `docker` 命令加 `sudo`。

```bash
docker run --rm hello-world
```

## 4. 获取代码、设置目录权限

以下用 Git URL 占位，替换为实际仓库地址；也可通过企业制品库上传受控源码包。不要把 `.env`、数据库备份或渠道密钥提交到仓库。

```bash
sudo mkdir -p /opt/customer-service /srv/customer-service/{mysql,redis,rabbitmq,minio,backups,release}
sudo chown -R "$USER":"$USER" /opt/customer-service /srv/customer-service
sudo chmod 700 /srv/customer-service/backups /srv/customer-service/mysql /srv/customer-service/redis /srv/customer-service/rabbitmq /srv/customer-service/minio
cd /opt/customer-service
git clone <你的 Git 仓库地址> .
git status --short
git log -1 --oneline
```

确认以下部署文件存在：

```bash
ls deploy/docker-compose.yml \
  deploy/docker-compose.prod.yml \
  deploy/nginx-gateway.conf.template \
  deploy/nginx-host.conf.example \
  deploy/minio-policy.json \
  deploy/migrations
```

生产持久化数据在 `/srv/customer-service/`，重建 `/opt/customer-service` 中的代码不会自动删除数据。`docker compose down -v` 会尝试删除 Compose 声明卷，本文的生产覆盖配置使用宿主机目录，但日常仍禁止使用 `-v` 作为升级命令。

## 5. 生成密钥和配置生产环境

先生成独立随机值并安全存入企业密码管理器。输出只用于本次配置，不要截图、写进 Git 或打印到应用日志。

```bash
openssl rand -hex 32  # SESSION_SECRET
openssl rand -hex 32  # MYSQL_ROOT_PASSWORD
openssl rand -hex 32  # MYSQL_APP_PASSWORD
openssl rand -hex 32  # REDIS_PASSWORD
openssl rand -hex 32  # RABBITMQ_PASSWORD
openssl rand -hex 32  # MINIO_ROOT_PASSWORD
openssl rand -hex 32  # MINIO_APP_PASSWORD / OBJECT_STORAGE_SECRET_KEY
openssl rand -hex 16  # MINIO_ROOT_USER，可直接使用随机字符串
```

复制模板并限制权限：

```bash
cd /opt/customer-service
cp .env.example .env
chmod 600 .env
```

将 `.env` 改为实际值。`LOCAL_MYSQL_DSN` 和 `LOCAL_AMQP_URL` 是容器内连接，主机名必须是 `mysql`、`rabbitmq`，不能写 `localhost`。AMQP URL 中密码如出现 `@`、`:`、`/`、`%` 等字符，必须进行 URL 编码。MySQL DSN 不是普通 URL：用户名、密码按 `go-sql-driver/mysql` 的 DSN 规则处理，密码可包含除特定分隔符外的字符，不能做 URL percent-encoding；建议使用独立随机十六进制密码，避免转义歧义。

示例（尖括号内容全部替换，不能照抄弱密码）：

```dotenv
APP_ENV=production
LOCAL_MYSQL_DSN=app:<MYSQL_APP_PASSWORD原始值>@tcp(mysql:3306)/customer_service?charset=utf8mb4&parseTime=true&loc=UTC
MYSQL_ROOT_PASSWORD=<独立随机值>
MYSQL_APP_PASSWORD=<独立随机值>
REDIS_PASSWORD=<独立随机值>
LOCAL_AMQP_URL=amqp://app:<URL转义后的RABBITMQ_PASSWORD>@rabbitmq:5672/
RABBITMQ_PASSWORD=<独立随机值>
CONSOLE_BASE_URL=https://console.example.com
SESSION_SECRET=<至少32字节的随机值，建议64个十六进制字符>

OBJECT_STORAGE_ENDPOINT=http://minio:9000
OBJECT_STORAGE_PUBLIC_ENDPOINT=https://storage.example.com
OBJECT_STORAGE_BUCKET=customer-service
OBJECT_STORAGE_ACCESS_KEY=customer-service-app
OBJECT_STORAGE_SECRET_KEY=<对应密钥>
WEB_STORAGE_ENDPOINT=http://minio:9000
MINIO_ROOT_USER=<独立随机值>
MINIO_ROOT_PASSWORD=<独立随机值>
MINIO_APP_PASSWORD=<独立随机值，与OBJECT_STORAGE_SECRET_KEY相同>

DINGTALK_CORP_ID=<企业CorpID>
DINGTALK_APP_KEY=<钉钉应用Client ID/AppKey>
DINGTALK_APP_SECRET=<钉钉应用密钥>
DINGTALK_CARD_TEMPLATE_ID=<互动卡片模板ID>
DINGTALK_CARD_API_SECRET=<卡片回调验签密钥>
DINGTALK_CALLBACK_ROUTE_KEY=<与卡片配置一致的路由Key>
DINGTALK_CALLBACK_TOKEN=<钉钉事件回调Token>
DINGTALK_CALLBACK_AES_KEY=<钉钉事件回调AES Key>

WECHAT_SERVICE_TOKEN=<服务号回调Token>
WECHAT_SERVICE_AES_KEY=<服务号EncodingAESKey>
WECHAT_SERVICE_APP_SECRET=<服务号AppSecret>
WECHAT_MINIAPP_TOKEN=<小程序回调Token，无此渠道时留空>
WECHAT_MINIAPP_AES_KEY=<小程序EncodingAESKey>
WECHAT_MINIAPP_APP_SECRET=<小程序AppSecret>

VITE_DINGTALK_CORP_ID=<与DINGTALK_CORP_ID一致>
VITE_DINGTALK_CLIENT_ID=<与DINGTALK_APP_KEY一致>
OTEL_EXPORTER_OTLP_ENDPOINT=
WECHAT_COMMENT_SOURCES_JSON=[]
```

注意：

1. `.env` 只供 Compose 在启动时读取，Docker 容器构建阶段不会自动获得前端 `VITE_*` 变量，因此第 6 节显式传入构建参数。前端只能包含 CorpID、Client ID 等公开标识，绝不能包含 AppSecret、AES Key、Token 或 Session Secret。
2. 若使用阿里云 RDS、云数据库 Redis、消息队列 RabbitMQ、OSS 等托管服务，应另行编写覆盖文件，移除对应本地容器和初始化依赖，并修改连接地址、TLS、账号与网络策略；不能仅改 `.env` 就假定托管服务已经接入。
3. 当前对象存储实现使用 S3 兼容 SDK。使用 OSS 时必须验证其 S3 兼容端点、路径风格/虚拟主机风格和签名行为，不能直接把任意 OSS URL 当作 MinIO 等价替换。
4. `WECHAT_COMMENT_SOURCES_JSON` 是 JSON 数组，元素字段为 `accountKey`、`msgDataID`、`articleIndex`、`cursor`。不需要评论定时同步时保持 `[]`。
5. `OBJECT_STORAGE_PUBLIC_ENDPOINT` 必须是独立存储域名的 HTTPS Origin，例如 `https://storage.example.com`，不能包含 `/storage` 或其他路径。API 直接按该 Host 和 `/customer-service/<objectKey>` 生成 V4 签名；两层 Nginx 必须保留请求 URI 和 Host。先配置存储域名 DNS、证书和代理，再首次启动 `minio-init` 创建 Bucket；MinIO 使用默认 `us-east-1` 区域，网关不跟随跨域重定向。若已有 Bucket 位于其他区域，须先验证 MinIO 返回的重定向 Host/URI，不能直接当公网 URL 使用。

检查配置是否缺少必要字段，但不输出密码：

```bash
docker compose --env-file .env \
  -f deploy/docker-compose.yml \
  -f deploy/docker-compose.prod.yml config --quiet
```

不要将完整 `docker compose config` 输出发到群聊、工单或日志系统，它会包含环境变量密钥。

## 6. 构建镜像并配置宿主机 HTTPS

```bash
cd /opt/customer-service
export COMPOSE_FILE=deploy/docker-compose.yml:deploy/docker-compose.prod.yml
export COMPOSE_PROJECT_NAME=customer-service
export RELEASE_TAG="$(git describe --tags --always --dirty)"
printf '将 RELEASE_TAG=%s 写入 .env（后续运维固定使用该版本）\n' "$RELEASE_TAG"
# 审核后编辑 .env，不要直接在共享终端打印任何 Secret。

# VITE_* 只包含公开标识，不包含任何 Secret。
docker compose --env-file .env build \
  --build-arg VITE_DINGTALK_CORP_ID='<实际CorpID>' \
  --build-arg VITE_DINGTALK_CLIENT_ID='<实际ClientID>'
```

## 7. 配置宿主机 HTTPS

### 7.1 安装证书

将证书链与私钥放到仅 root 可读的目录，不能使用自签名证书做真实微信/钉钉回调：

```bash
sudo mkdir -p /etc/nginx/tls
sudo install -m 600 /path/to/fullchain.pem /etc/nginx/tls/fullchain.pem
sudo install -m 600 /path/to/privkey.pem /etc/nginx/tls/privkey.pem
sudo install -m 600 /path/to/storage-fullchain.pem /etc/nginx/tls/storage-fullchain.pem
sudo install -m 600 /path/to/storage-privkey.pem /etc/nginx/tls/storage-privkey.pem
```

使用阿里云 SSL 证书服务、ACME 客户端或企业证书自动化工具均可。证书续期后必须 reload Nginx，并监控到期时间。

### 7.2 配置反向代理

```bash
cd /opt/customer-service
sudo cp deploy/nginx-host.conf.example /etc/nginx/conf.d/customer-service.conf
sudo editor /etc/nginx/conf.d/customer-service.conf
```

至少替换：

- `console.example.com`：实际工作台 HTTPS 域名；
- `storage.example.com`：独立存储 HTTPS 域名，模板已包含独立的 `server` 块；它只代理 `/customer-service/`，不代理 MinIO 管理 API；
- `/etc/nginx/tls/fullchain.pem`、`privkey.pem`、`storage-fullchain.pem`、`storage-privkey.pem`：实际证书路径，工作台和存储域名均需被证书覆盖；
- 如默认站点占用域名，移除或调整 `/etc/nginx/nginx.conf` 和 `/etc/nginx/conf.d/` 中冲突的 `server` 块，不要删除未知业务配置。

```bash
sudo nginx -t
sudo systemctl reload nginx
```

SELinux 若处于 enforcing，允许 Nginx 代理到本机端口：

```bash
getenforce
sudo setsebool -P httpd_can_network_connect 1
```

容器网关启动前，工作台健康检查会暂时返回 502，这是正常的；完成第 7.3 节后再验证。公网 DNS 未生效前不能继续初始化 Bucket 或配置渠道回调。

### 7.3 首次迁移、启动与存储初始化

生产启动时 `migrate` 容器先获取数据库迁移锁并按版本执行 `deploy/migrations/*.up.sql`；成功后 Console、Ingress、Scheduler 才启动。重复运行时已记录的版本会跳过，不会重放 SQL。此步骤前必须已完成两个域名的 DNS、证书和 Nginx 配置。

```bash
cd /opt/customer-service
# 先仅执行迁移并检查输出。
docker compose --env-file .env run --rm migrate

# 首次启动，包括私有 Bucket 和最小权限服务账号的初始化。
docker compose --env-file .env up -d
docker compose --env-file .env ps
```

不要在没有备份的已有数据库上直接执行升级。首次部署若失败，先看第 12 节，不要通过删除数据库、手工改迁移版本表或修改已发布 migration 来“跳过错误”。

检查内部服务：

```bash
docker compose --env-file .env exec -T mysql sh -c \
  'mysql -uapp -p"$MYSQL_PASSWORD" customer_service -e "SELECT version, applied_at FROM schema_migration ORDER BY version"'

curl -fsS http://127.0.0.1:8088/healthz
curl -fsS http://127.0.0.1:8080/api/v1/health
curl -fsS https://console.example.com/api/v1/health
curl -fsS https://storage.example.com/storage-healthz
```

`MYSQL_PASSWORD` 已在数据库容器中设置，查询只在容器内使用它，不会把真实密码打印到终端。不要把包含 Secret 的完整容器环境导出到工单或普通日志。

### 7.4 核对对象存储最小权限账号

首次启动时 `minio-init` 自动完成：创建私有 Bucket、设置禁止匿名访问、创建 `OBJECT_STORAGE_ACCESS_KEY` 对应的独立服务账号，并绑定 `deploy/minio-policy.json` 中的最小权限策略。`MINIO_APP_PASSWORD` 必须与 `OBJECT_STORAGE_SECRET_KEY` 相同；应用不使用 MinIO root 凭据。

检查 Bucket、策略和服务账号：

```bash
docker compose --env-file .env run --rm --no-deps \
  --entrypoint /bin/sh minio-init -ec '
    mc alias set local http://minio:9000 "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD"
    mc anonymous get "local/$OBJECT_STORAGE_BUCKET"
    mc admin policy info local customer-service-media
    mc admin user info local "$OBJECT_STORAGE_ACCESS_KEY"
  '
```

轮换 MinIO 服务密钥时，先通过密码管理器生成新值，同步修改 `.env` 的 `MINIO_APP_PASSWORD`（Compose 将它注入为应用的 `OBJECT_STORAGE_SECRET_KEY`），重建 `minio-init` 以更新账号密码，最后重建 `console-api` 和 `scheduler`。在低峰期轮换，旧客户端中的已签发上传 URL 最长有效期为 10 分钟。

不要把 MinIO root 密钥放入应用日志、前端或普通运维脚本。`mc admin policy create` 在旧版本 `mc` 中可能使用 `mc admin policy add`，以镜像内 `mc admin policy --help` 为准。首次启动后不要随意删除 `minio-init`，应用启动依赖它的完成状态。

## 8. 初始化渠道、员工、团队、排班和路由

当前没有完整的渠道账号/员工/团队/路由管理 API；以下初始数据需要由 DBA 通过受审核的 SQL 初始化。`sla_policy` 当前也不是完整可配置 SLA 的启用入口，插入它不会改变代码中的现有 SLA 行为。

在迁移完成后执行。以下 SQL 只保存密钥的 `env://` 引用，不保存真实密钥。ID 可用固定 UUID；非测试环境不要复用示例中的 AppID、员工标识。

创建 `/opt/customer-service/bootstrap.sql`（权限设为 `600`），写入以下 SQL 并通过 Compose 的 stdin 执行，避免把 SQL 或凭据直接暴露在进程参数中：

```bash
docker compose --env-file .env exec -T mysql sh -c \
  'mysql -uapp -p"$MYSQL_PASSWORD" customer_service' < bootstrap.sql
```

不要将真实 Secret 写入 `bootstrap.sql`。SQL 内容：

```sql
START TRANSACTION;

-- 服务号：accountKey = service，回调 URL 使用该 key。
INSERT INTO channel_account
(id, account_key, channel, app_id, token_secret_ref, aes_key_secret_ref, app_secret_ref, status, created_at)
VALUES
('11111111-1111-7111-8111-111111111111', 'service', 'wechat_service', '<服务号AppID>',
 'env://WECHAT_SERVICE_TOKEN', 'env://WECHAT_SERVICE_AES_KEY', 'env://WECHAT_SERVICE_APP_SECRET', 'active', UTC_TIMESTAMP(3));

-- 无小程序时删除这条语句。
INSERT INTO channel_account
(id, account_key, channel, app_id, token_secret_ref, aes_key_secret_ref, app_secret_ref, status, created_at)
VALUES
('22222222-2222-7222-8222-222222222222', 'miniapp', 'wechat_miniapp', '<小程序AppID>',
 'env://WECHAT_MINIAPP_TOKEN', 'env://WECHAT_MINIAPP_AES_KEY', 'env://WECHAT_MINIAPP_APP_SECRET', 'active', UTC_TIMESTAMP(3));

-- 一名管理员。dingtalk_user_id 必须是钉钉员工 userId，不是 unionId、手机号或姓名。
INSERT INTO agent(id, dingtalk_user_id, name, role, active, created_at)
VALUES('33333333-3333-7333-8333-333333333333', '<钉钉员工userId>', '<管理员姓名>', 'admin', TRUE, UTC_TIMESTAMP(3));

-- 一个技能组及其成员。
INSERT INTO team(id, name, created_at)
VALUES('44444444-4444-7444-8444-444444444444', '客服一组', UTC_TIMESTAMP(3));
INSERT INTO team_agent(team_id, agent_id)
VALUES('44444444-4444-7444-8444-444444444444', '33333333-3333-7333-8333-333333333333');

-- 一个初始排班，时间请改为实际值班区间。
INSERT INTO shift(id, agent_id, team_id, start_at, end_at, state, created_at)
VALUES('55555555-5555-7555-8555-555555555555', '33333333-3333-7333-8333-333333333333',
       '44444444-4444-7444-8444-444444444444', '<UTC开始时间>', '<UTC结束时间>', 'active', UTC_TIMESTAMP(3));

-- 初始路由：priority 越大越先匹配。空 channel/accountId/tag 表示不限。
-- startMinute=0,endMinute=0 表示全天；其他区间按 UTC 的分钟数判断。
INSERT INTO routing_rule(id, priority, match_json, target_team_id, active, created_at)
VALUES('66666666-6666-7666-8666-666666666666', 100,
       JSON_OBJECT('channel', 'wechat_service', 'accountId', '', 'tag', '', 'startMinute', 0, 'endMinute', 0),
       '44444444-4444-7444-8444-444444444444', TRUE, UTC_TIMESTAMP(3));

COMMIT;
```

管理员可通过 `POST /api/v1/admin/shifts` 新增排班，必须使用管理员 JWT；当前没有公开初始化管理员账号密码的方式，身份来自钉钉免登及上述 `agent` 映射。

修改数据库前先备份，变更后检查：

```sql
SELECT account_key, channel, app_id, token_secret_ref, aes_key_secret_ref, app_secret_ref, status FROM channel_account;
SELECT id, dingtalk_user_id, role, active FROM agent;
SELECT * FROM team_agent;
SELECT * FROM shift;
SELECT id, priority, match_json, target_team_id, active FROM routing_rule;
```

此查询只允许出现 `env://...` 形式的 Secret 引用。若出现真实 Token、AppSecret、AES Key，立即停止上线，清理并轮换密钥。

## 9. 微信后台配置

以微信公众平台/小程序当前官方界面为准；本手册不替代权限和消息类型核验。

| 账号 | 回调 URL | channel |
| --- | --- | --- |
| 服务号 | `https://console.example.com/callbacks/wechat/service` | `wechat_service` |
| 小程序 | `https://console.example.com/callbacks/wechat/miniapp` | `wechat_miniapp` |

1. Token、EncodingAESKey 与 `.env` 对应账号的值一致，数据库只保存对应 `env://` 引用。
2. 启用安全模式时，POST 验签使用 `msg_signature`；入口会解密并校验 AppID。
3. 将 ECS 的固定公网出口 IP 配置到微信接口 IP 白名单（若平台要求）。NAT 出口变化会导致 Access Token/API 调用失败。
4. 确认账号具有客服消息、文章评论等所需权限，检查最新接口限额、媒体类型和错误码。
5. 回调服务器必须能访问 `/callbacks/wechat/{accountKey}`，并允许 GET（URL 验证）与 POST（消息）。不要给该入口加 JWT 鉴权。
6. 只有启用的账号才能在数据库中登记；未启用的小程序不要创建错误的 `channel` 映射。

健康检查不能代替验签：配置 URL 时由微信发起 GET 验证，观察 Ingress 日志和返回结果。发送一条真实消息后检查 `inbox_dedup`、`message`、`outbox_event`，并确认重复回调不会生成重复消息。

## 10. 钉钉后台配置

当前需要配置以下入口：

| 用途 | URL |
| --- | --- |
| H5 工作台 | `https://console.example.com/` |
| 互动卡片回调 | `https://console.example.com/api/v1/callbacks/dingtalk/card` |
| 普通事件回调 | `https://console.example.com/api/v1/callbacks/dingtalk/events` |
| 通知中的会话深链接 | `https://console.example.com/dingtalk/conversations/{conversationId}` |

1. 创建企业内部应用，配置 H5 首页/工作台地址和回调地址；将应用可见范围授权给客服员工。
2. 开通免登、通讯录读取和互动卡片等实际调用所需权限；发布应用后重新确认权限是否生效。
3. 前端使用 `requestAuthCode({corpId, clientId})`；`VITE_DINGTALK_CLIENT_ID` 必须与后端 AppKey 使用同一应用身份。
4. `DINGTALK_CARD_API_SECRET` 用于卡片 HMAC 验签；普通事件使用 `DINGTALK_CALLBACK_TOKEN` 和 `DINGTALK_CALLBACK_AES_KEY`，三者不可混用。
5. 卡片模板参数至少应支持 `title`、`text`、`url`、`conversationId`、`kind`，动作与当前实现的 `claim`、`view`、`transfer` 保持一致。卡片回调字段和动作 ID 以当前钉钉官方规范及实际回调样本验收。
6. 配置 `DINGTALK_CALLBACK_ROUTE_KEY` 与卡片投递要求一致。回调未启用时也不要填写来源不明的密钥。
7. 普通浏览器打开页面会提示仅支持钉钉内免登，这是当前功能限制，不是 Nginx 故障。

## 11. 上线验收

按顺序验证，不要只检查容器处于 Up 状态。

### 11.1 服务与网络安全

```bash
docker compose --env-file .env ps
docker compose --env-file .env logs --since=15m --no-color console-api ingress scheduler
curl -fsS https://console.example.com/api/v1/health
curl -fsS https://storage.example.com/storage-healthz
ss -lnt
```

- Console、Ingress、Scheduler、RabbitMQ、Redis、MySQL、MinIO、Web、Gateway 均无持续重启。
- 外部不能连接数据库、Redis、RabbitMQ 管理台、MinIO 管理台及内部 HTTP 端口。
- 日志不得出现真实渠道密钥、Session Secret、完整 Access Token、钉钉 AppSecret 或数据库密码。
- Nginx 证书有效，HTTP 会跳转 HTTPS，HTTPS 使用受信任证书。

### 11.2 真实渠道闭环

- [ ] 微信测试账号发文本消息，工作台 5 秒内出现。
- [ ] 同一回调重复投递，消息和通知不重复。
- [ ] 指定钉钉员工收到互动卡片，签名领取成功，H5 深链接打开正确会话。
- [ ] 无权限员工无法访问/领取不属于自己的会话。
- [ ] 客服回复先显示 pending，最终状态变为已送达；微信用户实际收到。
- [ ] 文本、图片、语音分别测试，确认预签名上传 URL 为 `https://storage.example.com/customer-service/...`，浏览器 PUT 返回成功，后续客服消息能读取对应对象。
- [ ] 用户发消息场景的 48 小时窗口和 5 条配额在边界时拒绝发送，错误状态可见。
- [ ] 停止 RabbitMQ 时回调/写入和 Worker 行为符合预期，恢复后 Outbox 可继续处理；不能仅以“ACK 成功”认定消息已最终送达。
- [ ] 评论进入独立评论队列，不被创建为私信会话，也不伪造私信回复。
- [ ] 非排班客服不被自动分配，超时升级和通知不重复。

若媒体上传失败，首先确认浏览器实际请求地址、CORS、MinIO Bucket、原始 URI 和签名 Host。可用浏览器的预检命令验证允许的 Origin（URL 不能携带签名到日志系统）：

```bash
curl -i -X OPTIONS 'https://storage.example.com/customer-service/agent-media/probe.png' \
  -H 'Origin: https://console.example.com' \
  -H 'Access-Control-Request-Method: PUT'
```

预期返回 204，且 `Access-Control-Allow-Origin` 等于工作台 Origin；使用其他 Origin 时不应返回允许头。预检成功不代表签名 PUT 已通过，仍须执行真实上传验收。当前返回 URL 是独立存储域名的 S3 预签名地址，不是公开下载目录；Bucket 必须保持私有。`/customer-service/` 代理只允许已签名的对象操作，不能开放 MinIO 管理 API。

### 11.3 验收边界

单测、Mock 和容器健康状态不等于生产验收。真实账号权限、5 秒进线、实际微信送达、钉钉通知/H5、媒体上传、浏览器 OAuth、完整路由管理、可配置 SLA、完整工单升级、补偿任务和运营报表仍需逐项确认。没有真实渠道验收结果时，不应宣称 P0–P4 全部完成。

## 12. 日常运维与故障排查

### 12.1 常用命令

```bash
cd /opt/customer-service
export COMPOSE_FILE=deploy/docker-compose.yml:deploy/docker-compose.prod.yml

docker compose --env-file .env ps
docker compose --env-file .env logs --tail=200 console-api
docker compose --env-file .env logs --tail=200 ingress
docker compose --env-file .env logs --tail=200 scheduler
docker compose --env-file .env logs --since=30m mysql rabbitmq redis minio
docker compose --env-file .env restart console-api
sudo nginx -t && sudo systemctl reload nginx
sudo journalctl -u nginx --since '30 minutes ago'
```

Scheduler 的 `/health` 和 `/metrics` 仅容器网络内可访问。不要为图方便将它们直接暴露公网；可通过受控端口转发或企业监控代理访问：

```bash
docker compose --env-file .env exec -T gateway \
  wget -qO- http://scheduler:8082/health
```

### 12.2 症状定位

| 症状 | 优先检查 |
| --- | --- |
| 容器不断重启 | `docker compose logs <服务>`、DSN/Redis/MQ/MinIO 凭据、迁移错误、磁盘和内存 |
| `migrate` 失败 | 数据库权限、网络、版本表、DDL 错误；保留错误现场，不要删除 `schema_migration` |
| 微信 URL 验证失败 | HTTPS/DNS、Ingress 路由、账号 `accountKey`、Token、服务器时间、签名参数 |
| 微信消息 403 | `signature`/`msg_signature` 使用场景、Token、EncodingAESKey、AppID、回调原始请求体 |
| 微信消息 503 | MySQL 可用性、迁移、连接超时；不得将失败 ACK 当作成功处理 |
| 有消息但没有钉钉通知 | Scheduler、RabbitMQ 消费、卡片模板、员工 `userId`、AppKey/AppSecret、卡片 Secret |
| H5 免登失败 | 应用可见范围、CorpID/Client ID、H5 地址、后端 AppKey、agent 映射和 active 状态 |
| 回复 pending/失败 | `message.status/error_code/error_message`、微信 Access Token、回复窗口、配额、Scheduler 与 Redis |
| 附件上传失败 | 存储域名 DNS/HTTPS、`/customer-service/` 代理、MinIO 健康状态、Bucket、CORS、原始 URI/签名 Host、文件类型和 20 MiB 限制 |
| 迁移锁等待 | 是否有另一个 migrate 正在执行；避免并发运行手工迁移和启动迁移 |

建议由 DBA 定期只读检查：

```sql
SELECT status, COUNT(*) FROM message GROUP BY status;
SELECT COUNT(*) AS pending_outbox FROM outbox_event WHERE published_at IS NULL;
SELECT status, COUNT(*) FROM dingtalk_delivery GROUP BY status;
SELECT received_at, COUNT(*) FROM inbox_dedup GROUP BY received_at ORDER BY received_at DESC LIMIT 10;
```

当前没有完整的失败事件补偿后台和死信处理管理台，持续积压、未知事件或 `failed_final` 不能简单删除；必须保留事件、日志和上下文，按经过审核的补偿流程处理。

## 13. 备份、恢复与升级

### 13.1 备份范围

至少备份：

- MySQL 全库：会话、消息、Outbox、幂等、员工映射和配置；
- `/srv/customer-service/minio/`：附件对象；
- `/srv/customer-service/rabbitmq/`：队列持久化数据；
- `.env` 和企业 Secret Manager 中的配置：离线加密保存，不能与普通备份放在同一权限域；
- 发布版本号、镜像摘要、migration 版本。

MySQL 一致性备份示例：

```bash
cd /opt/customer-service
export COMPOSE_FILE=deploy/docker-compose.yml:deploy/docker-compose.prod.yml
docker compose --env-file .env exec -T mysql sh -c \
  'exec mysqldump -uroot -p"$MYSQL_ROOT_PASSWORD" --single-transaction --routines --triggers --set-gtid-purged=OFF customer_service' \
  > "/srv/customer-service/backups/customer-service-$(date +%Y%m%d-%H%M%S).sql"
chmod 600 /srv/customer-service/backups/*.sql
```

将备份复制到独立的加密对象存储/异地备份，并配置保留策略、访问审计和定期恢复演练。仅在 ECS 本机留备份不能应对主机丢失。MinIO/RabbitMQ 文件备份应在受控停机或经批准的存储快照方式下进行，避免直接复制正在写入的文件后误认为一致备份。

### 13.2 升级顺序

```bash
cd /opt/customer-service
export COMPOSE_FILE=deploy/docker-compose.yml:deploy/docker-compose.prod.yml

# 1. 记录当前版本，完成并验证备份。
git rev-parse HEAD
docker compose --env-file .env ps

# 2. 在预发布环境用生产数据副本验证新版本和迁移。
git fetch --all
# 审核提交与 deploy/migrations 后，切换到指定发布标签，不要盲目跟随最新分支。
git checkout <已批准的发布标签或提交>

# 3. 固定本次发布标签，构建全部镜像并执行一次迁移。
export RELEASE_TAG="$(git describe --tags --always --dirty)"
printf '将 RELEASE_TAG=%s 写入 .env（后续运维固定使用该版本）\n' "$RELEASE_TAG"
# 审核后同步到 .env，确保后续运维命令复用相同镜像。
docker compose --env-file .env build \
  --build-arg VITE_DINGTALK_CORP_ID='<实际CorpID>' \
  --build-arg VITE_DINGTALK_CLIENT_ID='<实际ClientID>'
docker compose --env-file .env run --rm migrate

# 4. 单机短暂重建服务；低峰期执行，检查日志和业务指标。
docker compose --env-file .env up -d --no-deps --force-recreate console-api ingress scheduler web gateway
docker compose --env-file .env ps
curl -fsS https://console.example.com/api/v1/health
```

当前 migration 只有向前脚本，不能假设执行 `down` 可以恢复旧 schema。发布失败时先停止有问题的发布、保留数据，使用旧版本镜像和经过演练的数据库恢复方案回滚；不要在生产库反向手工修改 migration。

### 13.3 恢复演练

在隔离环境创建空库，执行目标版本 migration 后恢复备份，验证账号映射、消息时间线、Outbox、附件和真实渠道。恢复到原生产库前必须停机、确认恢复点和回退方案，避免新旧 Worker 同时消费造成重复外部动作。

## 14. 生产加固与扩展建议

1. 生产优先使用 RDS MySQL、云 Redis、托管 RabbitMQ 和可靠对象存储，并启用内网访问、加密、备份、监控和高可用；本地容器是单机基线，不提供跨主机容灾。
2. 使用企业 Secret Manager 或受控部署系统下发密钥，定期轮换数据库、Redis、RabbitMQ、MinIO、Session 和渠道密钥。轮换 Session Secret 会使现有会话失效。
3. 为 MySQL、RabbitMQ、MinIO 磁盘配置容量告警和独立备份策略；不要依赖 `latest` 标签作为长期供应链基线，构建后固定镜像摘要。
4. 定期运行 `make test`、`make lint`、`make generate-check` 和 CI 的真实 MySQL/Redis/RabbitMQ/MinIO 集成测试。主机部署不需要安装开发工具链，CI 负责构建和验证。
5. 在受控监控网络采集 `/metrics`，监控回调失败率、队列积压、消息最终失败、Token 刷新、SLA、磁盘和证书过期。
6. 扩容时不要简单重复启动多个 Scheduler 而不检查消费能力。当前 RabbitMQ consumer 使用固定名称 `customer-service` 且未显式设置 QoS/多 worker 订阅；水平扩展前需完成消费语义与压测验证。
7. 浏览器 OAuth、完整管理 API、可配置 SLA、补偿任务、死信治理等未完成能力应先开发并验收，不要通过手工 SQL 长期替代正式运营流程。
