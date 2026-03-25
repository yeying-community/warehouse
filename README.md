
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

## HA 命令行

编译后的 `build/warehouse` 除了可以启动服务，也可以作为运维 CLI 使用：

```shell
# 查看当前实例的复制状态
build/warehouse ha status -c config.yaml

# 在 active 上查看某个 standby 的复制状态
build/warehouse ha status -c config.yaml --target-node-id warehouse-standby-1

# 直接查看某个 standby 实例自己的复制状态
build/warehouse ha status -c config.yaml --peer --target-node-id warehouse-standby-1

# 查看当前节点相关的 assignment 状态
build/warehouse ha assignments status -c config.yaml

# 手工触发某个 standby 的历史补齐
build/warehouse ha reconcile start -c config.yaml --target-node-id warehouse-standby-1

# 查看某个 standby 的历史补齐状态
build/warehouse ha reconcile status -c config.yaml --target-node-id warehouse-standby-1

# 显式写入某个 standby 的 bootstrap baseline
build/warehouse ha bootstrap mark -c config.yaml --peer --target-node-id warehouse-standby-1 --outbox-id 123
```

说明：
- CLI 会自动根据 `config.yaml` 构造 internal 签名请求，不需要手工写 `curl`、shell 脚本或 HMAC header
- 默认访问当前实例的 internal 地址
- `--target-node-id` 用于按 standby 精确观察或操作目标；在多 standby 场景下建议显式指定
- 传 `--peer` 时会从 PostgreSQL 控制面解析当前 effective assignment 对应的 peer，并直接访问该 peer 的 internal 地址
- 在多 standby 场景下，推荐把 `--peer` 和 `--target-node-id` 一起使用；如果只传 `--peer`，当前会使用控制面解析出的第一个匹配 peer
- 也可以通过 `--base-url` 显式指定目标实例
- `bootstrap mark` 现在要求携带当前 assignment generation；推荐使用 `--peer --target-node-id <standby-id>`，CLI 会自动补齐内部 header
- `build/warehouse ha assignments status` 直接读取 PostgreSQL 控制面表，不走 HTTP；当前可以直接观察 active 侧 assignment allocator 写入的 lease / generation / state

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
- 只覆盖本地双实例必需字段：端口、节点身份、replication 和数据目录
- 同时会自动补齐 `node.advertise_url`，让 active / standby 通过共享控制面自动发现彼此
- 可通过环境变量覆盖本地端口和 internal shared secret，例如 `ACTIVE_PORT`、`STANDBY_PORT`、`INTERNAL_SHARED_SECRET`
- active / standby 不再配置静态 `peer_node_id` / `peer_base_url`；运行时复制统一按 effective assignment 解析 peer，再通过 `cluster_nodes` 中的 `advertise_url` 补齐目标 URL
- standby 的 internal apply / reconcile / bootstrap 请求会校验当前 effective assignment，只接受当前 assigned active

## 高可用部署提示

如果准备落地 `1 active + N standby`（最小可以先从 `1 standby` 起步）：

- `webdav.directory` 应指向每台机器自己的本地数据盘挂载目录
- 当前阶段一推荐路线是：active 对外、standby 仅 internal，同步本地文件数据
- 建议为每个实例设置唯一的 `node.id`，并设置 `node.advertise_url` 作为 internal 可达地址，供共享控制面发现
- 生产环境建议设置 `webdav.auto_create_directory: false`
- 切换前除了检查 `/api/v1/public/health/readiness`，还要检查复制状态
- 详细说明参考 [docs/zh/ha-active-standby-deployment.md](docs/zh/ha-active-standby-deployment.md)

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

### 使用“目录授权密钥”挂载（推荐）

如果你希望只挂载某个目录、避免暴露账号全量权限，推荐使用“访问密钥 + 目录授权”：

1. 在“个人中心 -> 访问密钥”里新建密钥，保存 `Key ID` 和 `Key Secret`。
2. 在资产列表中选中目标目录，点击“授权密钥”，把该密钥授权到该目录。
3. 挂载时，`url` 直接使用“已授权目录”的 WebDAV 地址；`username` 用 `Key ID`，`password` 用 `Key Secret`。

```shell
# 示例：仅挂载 /personal/projects/demo 目录
bash scripts/mount_davfs.sh mount \
  https://example.com/dav/personal/projects/demo \
  /mnt/demo \
  ak_41dd9a3b7f5b1c2d \
  sk_5a79c9e8d0f1b2c3d4e5f60718293a4b5c6d7e8f9a0b1c2d

# 配置开机自动挂载
bash scripts/mount_davfs.sh install-fstab \
  https://example.com/dav/personal/projects/demo \
  /mnt/demo \
  ak_41dd9a3b7f5b1c2d \
  sk_5a79c9e8d0f1b2c3d4e5f60718293a4b5c6d7e8f9a0b1c2d
```

补充说明：
- 访问密钥建议挂载“已授权目录路径”，不要直接挂载 `/dav` 根路径。
- 一个密钥可以授权多个目录；如需分别挂载，给每个目录使用独立的 `url` 和本地挂载点即可。
- 如果密钥被撤销，或目录授权被取消，挂载点后续访问会返回认证/权限错误，需要恢复授权后重新挂载。
- 如果密钥权限只有 `read + create`，更适合原始 WebDAV 直传“新增文件但不覆盖”的场景。
- 如果要通过 `davfs2` 挂载后直接 `cp` / 拖拽文件，通常至少需要 `read + create + update`；因为挂载客户端可能会发出带“更新语义”的请求，而不是单次新建 `PUT`。
- 如果你的目标是“绝不覆盖已有文件”，建议把挂载点作为只读浏览使用，新增文件改为走直传命令，而不是通过挂载目录写入。
