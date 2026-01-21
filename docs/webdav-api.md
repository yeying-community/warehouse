# WebDAV 文件 CRUD API（简明版）

本文档面向“文件增删改查”，重点覆盖 WebDAV 操作与常用辅助接口。

## 1. 基本信息

- Base URL：`http(s)://<host>:<port>`
- WebDAV 前缀：来自 `webdav.prefix`（默认为 `/`）
  - 例如 `webdav.prefix: "/dav"`，则 WebDAV 路由为 `/dav/`
- 每个用户的根目录为其配置的用户目录（服务端自动映射）

## 2. 认证方式

WebDAV 请求支持两种方式：

1) **Bearer Token**（JWT 或 UCAN）

```
Authorization: Bearer <token>
```

2) **Basic Auth**（常见 WebDAV 客户端）

```
Authorization: Basic <base64(username:password)>
```

说明：
- Bearer Token 由 `/api/v1/public/auth/*` 获取（JWT），或由 UCAN 颁发方签发（UCAN）。
- UCAN 需在配置中开启 `web3.ucan.enabled: true`，并设置 `audience/resource/action` 与令牌能力匹配。

## 3. 认证接口流程（challenge / verify / refresh）

说明：认证接口统一使用以下路径。

- `/api/v1/public/auth/*`

响应格式为 SDK 统一结构：

```json
{
  "code": 0,
  "message": "ok",
  "data": { },
  "timestamp": 1710000000000
}
```

### 3.1 Challenge

- 方法：`GET` 或 `POST`
- 路径：`/api/v1/public/auth/challenge`

GET 参数：
- `address`（必填，以太坊地址）

POST Body：

```json
{ "address": "0x..." }
```

成功响应 `data`：

```json
{
  "address": "0x...",
  "challenge": "Sign this message to authenticate....",
  "nonce": "random-hex",
  "issuedAt": 1710000000000,
  "expiresAt": 1710000000000
}
```

说明：`challenge` 需用钱包签名，过期时间约 5 分钟。

### 3.2 Verify

- 方法：`POST`
- 路径：`/api/v1/public/auth/verify`

Body：

```json
{
  "address": "0x...",
  "signature": "0x..."
}
```

成功响应 `data`：

```json
{
  "address": "0x...",
  "token": "<access_token>",
  "expiresAt": 1710000000000,
  "refreshExpiresAt": 1710000000000
}
```

说明：
- 成功后会设置 `refresh_token` HttpOnly Cookie。
- `token` 作为访问 WebDAV 的 Bearer Token。

### 3.3 Refresh

- 方法：`POST`
- 路径：`/api/v1/public/auth/refresh`
- 依赖 Cookie：`refresh_token`

成功响应 `data`：

```json
{
  "address": "0x...",
  "token": "<access_token>",
  "expiresAt": 1710000000000,
  "refreshExpiresAt": 1710000000000
}
```

说明：刷新时会重新设置 `refresh_token` Cookie。

### 3.4 密码登录（可选）

- 方法：`POST`
- 路径：`/api/v1/public/auth/password/login`

Body：

```json
{
  "username": "alice",
  "password": "password123"
}
```

成功响应 `data`：

```json
{
  "address": "0x...",
  "username": "alice",
  "token": "<access_token>",
  "expiresAt": 1710000000000,
  "refreshExpiresAt": 1710000000000
}
```

说明：
- 用户必须已绑定钱包地址，否则会返回 `NO_WALLET`。
- 成功后会设置 `refresh_token` HttpOnly Cookie。

### 3.5 Logout

- 方法：`POST`
- 路径：`/api/v1/public/auth/logout`

成功响应 `data`：

```json
{
  "logout": true
}
```

说明：服务端会清理 `refresh_token` Cookie。

### 3.6 返回码字段说明

认证接口统一返回以下字段：

- `code`：业务码。当前实现中 **成功固定为 0**，错误时通常等于 **HTTP 状态码**（例如 400/401/404/500）。  
- `message`：人类可读信息（失败原因）。  
- `data`：成功数据体，失败时为 `null`。  
- `timestamp`：服务端毫秒时间戳。  

### 3.7 错误码示例

**示例 1：签名验证失败（HTTP 401）**

```json
{
  "code": 401,
  "message": "Signature verification failed",
  "data": null,
  "timestamp": 1710000000000
}
```

**示例 2：地址缺失或格式错误（HTTP 400）**

```json
{
  "code": 400,
  "message": "Address parameter is required",
  "data": null,
  "timestamp": 1710000000000
}
```

**示例 3：钱包地址未注册（HTTP 404）**

```json
{
  "code": 404,
  "message": "Wallet address not registered",
  "data": null,
  "timestamp": 1710000000000
}
```

说明：WebDAV 原生请求（如 PROPFIND/PUT/DELETE）通常只返回 HTTP 状态码与简单文本，具体错误以 HTTP 状态为准。

## 4. CRUD 方法矩阵

| 目的 | 方法 | 路径 | 说明 |
| --- | --- | --- | --- |
| 列表 | PROPFIND | `{prefix}/{path}` | 目录列举，返回 XML（207） |
| 读取 | GET / HEAD | `{prefix}/{path}` | 下载文件/获取元数据 |
| 新建/更新文件 | PUT | `{prefix}/{path}` | 同名覆盖即更新 |
| 新建目录 | MKCOL | `{prefix}/{path}/` | 创建空目录 |
| 删除 | DELETE | `{prefix}/{path}` | 进入回收站（非立即物理删除） |
| 重命名/移动 | MOVE | `{prefix}/{path}` | 需要 `Destination` 头 |
| 复制 | COPY | `{prefix}/{path}` | 需要 `Destination` 头 |

常用请求头：
- `Depth: 0|1|infinity`（PROPFIND）
- `Destination: <url-or-path>`（MOVE / COPY）
- `Overwrite: T|F`（MOVE / COPY，可选）
- `Content-Type`（PUT，可选）

## 5. 常用示例（curl）

以下示例以 Basic Auth 为例（Bearer 方式替换请求头即可）。

### 5.1 列出目录

```bash
curl -X PROPFIND -u alice:password123 \
  -H "Depth: 1" \
  http://127.0.0.1:6065/ | xq .
```

### 5.2 上传/更新文件

```bash
curl -X PUT -u alice:password123 \
  --data-binary @file.txt \
  http://127.0.0.1:6065/docs/file.txt
```

### 5.3 下载文件

```bash
curl -u alice:password123 \
  http://127.0.0.1:6065/docs/file.txt
```

### 5.4 创建目录

```bash
curl -X MKCOL -u alice:password123 \
  http://127.0.0.1:6065/docs/new-folder/
```

### 5.5 重命名/移动

```bash
curl -X MOVE -u alice:password123 \
  -H "Destination: http://127.0.0.1:6065/docs/renamed.txt" \
  http://127.0.0.1:6065/docs/file.txt
```

### 5.6 复制

```bash
curl -X COPY -u alice:password123 \
  -H "Destination: http://127.0.0.1:6065/docs/file-copy.txt" \
  http://127.0.0.1:6065/docs/file.txt
```

### 5.7 删除（进入回收站）

```bash
curl -X DELETE -u alice:password123 \
  http://127.0.0.1:6065/docs/renamed.txt
```

### 5.8 Bearer Token 示例

```bash
curl -X PROPFIND \
  -H "Depth: 1" \
  -H "Authorization: Bearer <token>" \
  http://127.0.0.1:6065/
```

### 5.9 密码登录与退出示例

```bash
# 密码登录（获取 access token，同时写入 refresh_token Cookie）
curl -X POST \
  -H "Content-Type: application/json" \
  -d '{"username":"alice","password":"password123"}' \
  http://127.0.0.1:6065/api/v1/public/auth/password/login

# 退出（清理 refresh_token Cookie）
curl -X POST \
  http://127.0.0.1:6065/api/v1/public/auth/logout
```

## 6. 回收站 API（可选）

DELETE 仅将文件移动到回收站，如需恢复或彻底删除可使用：

- `GET /api/v1/public/webdav/recycle/list`：回收站列表
- `POST /api/v1/public/webdav/recycle/recover`：恢复
  - Body：`{"hash":"<itemHash>"}`
- `DELETE /api/v1/public/webdav/recycle/permanent`：永久删除
  - Body：`{"hash":"<itemHash>"}`
- `DELETE /api/v1/public/webdav/recycle/clear`：清空回收站

示例：

```bash
# 获取回收站列表
curl -u alice:password123 \
  http://127.0.0.1:6065/api/v1/public/webdav/recycle/list

# 恢复
curl -X POST -u alice:password123 \
  -H "Content-Type: application/json" \
  -d '{"hash":"<itemHash>"}' \
  http://127.0.0.1:6065/api/v1/public/webdav/recycle/recover
```

## 7. 配额 API（可选）

- `GET /api/v1/public/webdav/quota`

```bash
curl -u alice:password123 \
  http://127.0.0.1:6065/api/v1/public/webdav/quota
```

## 8. 常见状态码

- `200/201/204`：成功
- `207 Multi-Status`：PROPFIND 成功（XML 响应）
- `401 Unauthorized`：未认证或 token 无效
- `403 Forbidden`：无权限
- `404 Not Found`：路径不存在
- `409 Conflict`：目录冲突或已存在
- `412 Precondition Failed`：条件不满足（如 Overwrite=F）
- `507 Insufficient Storage`：配额不足

## 9. 注意事项

- 路径请使用 URL 编码（空格、中文等需要编码）。
- `Destination` 可以是完整 URL 或绝对路径。
- 目录列举使用 `PROPFIND`，响应为 XML（建议配合 `xq` 查看）。
- 系统会忽略 `._*`、`.DS_Store`、`.AppleDouble`、`Thumbs.db` 等系统文件。
- WebDAV 支持 Unicode 路径。
