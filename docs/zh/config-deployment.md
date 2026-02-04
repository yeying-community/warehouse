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

## 关键配置块

- `server`：监听地址、端口、TLS、超时
- `database`：PostgreSQL 连接信息与连接池
- `webdav`：根目录、前缀、NoSniff
- `web3`：JWT 秘钥、Token 过期时间、UCAN 规则
- `security`：无密码模式、反向代理标记、管理员地址白名单
- `cors`：跨域设置

## 覆盖方式示例

```bash
# 1) 配置文件启动
webdav -c config.yaml

# 2) 使用命令行覆盖端口/目录
webdav -c config.yaml -p 8080 -d /data

# 3) 通过环境变量覆盖 JWT secret
export WEBDAV_JWT_SECRET="your-secret"
webdav -c config.yaml
```

## 部署方式

### 二进制直接部署

- `make` 构建后使用 `build/webdav` 启动
- 建议由 systemd/supervisor 进行守护

### 容器部署（Docker / Compose）

- `Dockerfile` 与 `docker-compose.yml` 提供容器化部署方式
- 关键点：
  - 挂载 `config.yaml`
  - 挂载数据目录（`webdav.directory`）
  - 确保 PostgreSQL 可访问

### 反向代理

- 通过 Nginx/Traefik 代理时建议设置 `security.behind_proxy=true`
- 若走 HTTPS 终止，确保 `X-Forwarded-Proto` 正确传递

## 数据持久化

- 文件数据：位于 `webdav.directory` 指定目录（建议挂载外部卷）
- 元数据：PostgreSQL（用户/分享/回收站/地址簿）

## 启动检查

- 健康检查：`/api/v1/public/health/heartbeat`
- WebDAV 访问：使用 Basic 或 Bearer Token
