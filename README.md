
# 搭建本地环境

## 启动PG数据库(如果没有PG数据库)

1. 下载代码库(deployer)[https://github.com/yeying-community/deployer]
2. 切换到`middleware/postgresql`目录下，参考`README.md`启动数据库

## 本地配置

```shell
# 基于模板创建配置文件
cp config.yaml.template config.yaml
# 修改config.yaml中的数据库配置
```

## 本地启动

```shell
# 启动时会自动检查并创建 webdav.directory 指定目录（如 ./test_data）
go run ./cmd/warehouse -c config.yaml

# 或者先编译后启动
go build -o build/warehouse ./cmd/warehouse
build/warehouse -c config.yaml

```

## 健康检查

```shell
curl http://127.0.0.1:6065/api/v1/public/health/heartbeat
curl http://127.0.0.1:6065/api/v1/public/health/readiness

# 或使用二进制直接做 readiness 检查
build/warehouse -c config.yaml --check-ready
```

## API 文档

- WebDAV 文件 CRUD 与认证流程：`docs/webdav-api.md`
- 认证接口统一使用 `/api/v1/public/auth/*`

## 设计文档

- 中文：`docs/zh/README.md`
- English: `docs/en/README.md`


# UCAN 认证

在 `config.yaml` 中启用 UCAN 后，可使用 `Authorization: Bearer <UCAN>` 访问需要鉴权的 API/WebDAV 资源。

```yaml
web3:
  jwt_secret: "your-super-secret-jwt-key-at-least-32-characters-long"
  token_expiration: 24h
  refresh_token_expiration: 720h
  ucan:
    enabled: true
    audience: "did:web:localhost:6065"
    required_resource: "profile"
    required_action: "read"
```

# 邮箱验证码登录

启用邮箱验证码登录需要在 `config.yaml` 中配置 SMTP，并把 `email.enabled` 设为 `true`：

```yaml
email:
  enabled: true
  smtp_host: "smtp.example.com"
  smtp_port: 587
  smtp_username: "user@example.com"
  smtp_password: "your-password"
  from: "noreply@example.com"
  from_name: "Warehouse"
  template_path: "resources/email/email_code_login_mail_template_zh-CN.html"
```

接口：
- 发送验证码：`POST /api/v1/public/auth/email/code`
- 邮箱登录：`POST /api/v1/public/auth/email/login`

# 常用命令行操作

```shell
# 1. 安装xq，用于格式化结果
# macOS
brew install libxml2
# Ubuntu/Debian
sudo apt-get install libxml2-utils

# 注意：以下示例默认使用 webdav.prefix=/dav；
# 如果你在 config.yaml 中改成了其他前缀，请替换为你的实际前缀。
# 详细变更清单见：docs/webdav-api.md（“修改 webdav.prefix 需要同步的地方”）
# 2. 列出目录（PROPFIND）
curl -s -X PROPFIND -u alice:password123  -H "Depth: 1"  http://127.0.0.1:6065/dav/ | xq .

# 3. 上传文件（PUT）
echo "Test content" | curl -X PUT -u alice:password123 --data-binary @-  http://127.0.0.1:6065/dav/upload.txt

# 4. 下载文件（GET）
curl -u alice:password123 http://127.0.0.1:6065/dav/upload.txt

# 5. 删除文件（DELETE）
curl -X DELETE -u alice:password123 http://127.0.0.1:6065/dav/upload.txt

# 6. 创建目录（MKCOL）
curl -X MKCOL -u alice:password123 http://127.0.0.1:6065/dav/new

# 7. 测试错误的密码
curl -u alice:wrongpassword http://127.0.0.1:6065/dav/

# 8. 查询quota使用情况
curl -u alice:password123 -s http://localhost:6065/api/v1/public/webdav/quota | jq .
```

# 常用的客户端操作

```text
MACOS
打开访达 -> 选择前往菜单 -> 连接服务器 -> 输入连接地址 -> 输入用户名和密码
```

# 脚本使用说明

## scripts/starter.sh

用于启动/停止/重启服务，默认无参数为 `start`：

```shell
# 启动
bash scripts/starter.sh

# 停止
bash scripts/starter.sh stop

# 重启
bash scripts/starter.sh restart
```

说明：
- 默认读取 `config.yaml`，若不存在则使用 `config.yaml.template`
- PID 文件：`run/warehouse.pid`
- 日志文件：`logs/warehouse.log`

## scripts/local.sh

用于本地快速拉起 active / standby 调试实例，使用 `go run` 前台启动：

```shell
# 默认启动 active
bash scripts/local.sh

# 显式启动 active
bash scripts/local.sh active

# 启动 standby
bash scripts/local.sh standby
```

说明：
- 使用前提是根目录 `config.yaml` 已经存在，并且 `go run ./cmd/warehouse -c config.yaml` 可以正常启动
- 脚本不会读取 `config.yaml.template`，只会从现有 `config.yaml` 复制并生成本地配置到 `.tmp/active.yaml` 或 `.tmp/standby.yaml`
- 数据目录分别使用 `.tmp/active/data` 和 `.tmp/standby/data`
- 默认端口为 `6065`（active）和 `6066`（standby）
- `.tmp/` 已加入 `.gitignore`
- 只覆盖本地双实例必需字段：端口、节点身份、internal replication 和数据目录
- 可通过环境变量覆盖本地端口和 internal shared secret，例如 `ACTIVE_PORT`、`STANDBY_PORT`、`INTERNAL_SHARED_SECRET`

## 高可用部署提示

如果准备落地 `1 active + 1 standby`：

- `webdav.directory` 应指向每台机器自己的本地数据盘挂载目录
- 当前阶段一推荐路线是：active 对外、standby 仅 internal，同步本地文件数据
- 生产环境建议设置 `webdav.auto_create_directory: false`
- 切换前除了检查 `/api/v1/public/health/readiness`，还要检查复制状态
- 详细说明参考 [docs/zh/ha-active-standby-deployment.md](docs/zh/ha-active-standby-deployment.md)

## scripts/bootstrap_standby.sh

用于在 standby 完成离线全量拷贝后，调用 internal 接口写入 baseline，并立即查询复制状态：

```shell
bash scripts/bootstrap_standby.sh \
  --standby-base-url https://warehouse-standby.internal \
  --source-node-id warehouse-active \
  --shared-secret replace-with-a-shared-internal-secret
```

如果要显式指定基线 outbox 序号：

```shell
bash scripts/bootstrap_standby.sh \
  --standby-base-url https://warehouse-standby.internal \
  --source-node-id warehouse-active \
  --shared-secret replace-with-a-shared-internal-secret \
  --outbox-id 12345
```

如果只想查看 standby 当前复制状态：

```shell
bash scripts/bootstrap_standby.sh \
  --standby-base-url https://warehouse-standby.internal \
  --source-node-id warehouse-active \
  --shared-secret replace-with-a-shared-internal-secret \
  --status-only
```

说明：
- 脚本会自动按 internal HMAC 规则构造签名，不需要手工计算 header
- 依赖 `curl`、`openssl`、`xxd`，若安装了 `jq` 会自动格式化 JSON 输出
- `--outbox-id` 不传时，standby 会使用当前 source -> standby 的最大 outbox 序号作为 baseline

## scripts/package.sh

用于构建前端 + 后端并生成安装包：

```shell
bash scripts/package.sh
```

说明：
- 会先构建前端产物到 `web/dist`
- 后端二进制输出为 `build/warehouse`
- 安装包输出到 `output/`

## scripts/mount_davfs.sh

用于 Linux 下通过 `davfs2` 将 WebDAV 目录挂载到本地目录，并支持 `fstab` 开机自动挂载：

```shell
# 一次性挂载
bash scripts/mount_davfs.sh mount https://example.com/dav /mnt/webdav alice

# 配置开机自动挂载（写入 /etc/fstab）
bash scripts/mount_davfs.sh install-fstab https://example.com/dav /mnt/webdav alice

# 取消开机自动挂载
bash scripts/mount_davfs.sh remove-fstab /mnt/webdav

# 卸载
bash scripts/mount_davfs.sh umount /mnt/webdav
```

说明：
- 缺少 `davfs2`（`mount.davfs`）时，脚本会在 Linux 上尝试自动安装
- 账号密码写入 `/etc/davfs2/secrets`（`600` 权限）
- `install-fstab` 默认写入 `nofail,_netdev`，避免开机网络未就绪导致启动失败
