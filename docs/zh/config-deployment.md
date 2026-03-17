# 配置与部署设计

本文档描述配置加载、运行时参数覆盖、以及常见部署方式。

## 配置来源与优先级

配置加载顺序：

1. **默认配置**：`config.DefaultConfig()`
2. **配置文件**：通过 `-c/--config` 指定的 YAML 文件
3. **命令行参数**：用于覆盖部分字段（地址/端口/TLS/目录等）
4. **环境变量**：用于覆盖部分字段（如 `WEBDAV_JWT_SECRET`）

> 最终配置以“后覆盖前”的方式生效。

## 配置校验要点

启动前会校验以下关键项（不通过则直接退出）：

- `web3.jwt_secret` 必填且至少 32 字符
- `database.type` 仅支持 `postgres` / `postgresql`
- `webdav.directory` 必须存在或可创建
- 启用 TLS 时必须提供 `cert_file` / `key_file`
- `email.enabled=true` 时需配置 SMTP 相关参数与模板路径

## 关键配置块

- `server`：监听地址、端口、TLS、超时
- `database`：PostgreSQL 连接信息与连接池
- `webdav`：根目录、前缀、目录自动创建、NoSniff
- `web3`：JWT 秘钥、Token 过期时间、UCAN 规则
- `email`：邮箱验证码登录（SMTP、模板、TTL、频率）
- `security`：无密码模式、反向代理标记、管理员地址白名单
- `cors`：跨域设置

## 覆盖方式示例

```bash
# 1) 配置文件启动
warehouse -c config.yaml

# 2) 使用命令行覆盖端口/目录
warehouse -c config.yaml -p 8080 -d /data

# 3) 通过环境变量覆盖 JWT secret
export WEBDAV_JWT_SECRET="your-secret"
warehouse -c config.yaml
```

## 部署方式

### 统一从 `config.yaml.template` 出发

- 建议以仓库根目录下的 `config.yaml.template` 为基础，生成当前环境自己的 `config.yaml`
- 部署时重点确认：
  - `database.*` 已指向可用的 PostgreSQL
  - `webdav.directory` 已指向真实数据目录
  - `web3.jwt_secret` 已替换为真实密钥
  - 阶段一 active/standby 场景下，`node.*` 与 `replication.*` 已正确填写

### 二进制直接部署

- 使用 `go build -o build/warehouse ./cmd/warehouse` 构建后，再通过 `build/warehouse` 启动
- 建议由 systemd/supervisor 或等效进程管理器进行守护

### 反向代理

- 通过 Nginx/Traefik 代理时建议设置 `security.behind_proxy=true`
- 若走 HTTPS 终止，确保 `X-Forwarded-Proto` 正确传递

## 数据持久化

- 文件数据：位于 `webdav.directory` 指定目录（建议挂载外部卷）
- 元数据：PostgreSQL（用户/分享/回收站/地址簿）

## 启动检查

- 健康检查：`/api/v1/public/health/heartbeat`
- 就绪检查：`/api/v1/public/health/readiness`
- CLI 就绪检查：`warehouse -c config.yaml --check-ready`
- WebDAV 访问：使用 Basic 或 Bearer Token

## 升级注意（WebDAV 目录访问密钥）

当版本包含“WebDAV 目录访问密钥”功能时，首次启动会自动执行数据库迁移，新增以下表及相关索引、触发器：

- `webdav_access_keys`
- `webdav_access_key_bindings`
- `idx_webdav_access_keys_owner_name`（同一用户密钥名唯一）

请确认：

- 运行服务的数据库账号具备 `CREATE TABLE / CREATE INDEX / CREATE TRIGGER` 权限。
- 升级后无需新增配置项，现有用户名密码登录方式保持不变。
- 即使暂不使用访问密钥，也不影响 active/standby 同步链路。

建议升级后执行一次自检：

```sql
SELECT COUNT(*) FROM webdav_access_keys;
SELECT COUNT(*) FROM webdav_access_key_bindings;
```

返回成功且无迁移报错，即表示升级完成。

## 阶段一高可用提示

如果正在落地 `1 active + N standby`（最小可以先从 `1 standby` 起步）：

- `webdav.directory` 应指向每台机器自己的本地数据盘挂载目录
- 各实例应使用相同的路径约定，并通过 `internal` 同步或其他复制机制保证文件多份
- `webdav.auto_create_directory` 应设为 `false`
- 流量切换不能只看 `readiness`，还应结合复制 lag / 最后应用序号
- 详细步骤参考：
  - [ha-active-standby-deployment.md](./ha-active-standby-deployment.md)
  - [internal-replication-design.md](./internal-replication-design.md)
