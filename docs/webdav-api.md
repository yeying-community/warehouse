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

说明（自动注册）：
- 若该钱包地址首次使用且未注册，服务端会在 `challenge` 阶段自动创建账号。
- 当前默认会创建随机用户名/目录并赋予默认权限与配额。
- 可通过 `web3.auto_create_on_challenge` / `web3.auto_create_on_ucan` 配置开关控制自动创建行为。
- 规划：后续会在自动创建前校验钱包是否持有足够额度的权益代币，不满足则不会自动创建。

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

### 3.8 非认证接口的返回格式说明

除认证接口外，大部分 JSON 接口 **不使用 SDK 统一结构**，常见两种风格：

1) **纯 JSON 业务对象**（成功时）

```json
{ "quota": 0, "used": 123, "available": -1, "percentage": 0, "unlimited": true }
```

2) **错误响应**

- 用户信息/配额接口：`{"error":"...","code":400,"success":false}`  
- 分享/地址簿等接口：多为 `http.Error`，返回 **纯文本** 错误信息  

因此建议客户端兼容：**先看 HTTP 状态码，再尝试 JSON 解析**。

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

响应示例：

```json
{
  "quota": 5368709120,
  "used": 1048576,
  "available": 5367660544,
  "percentage": 0.0195,
  "unlimited": false
}
```

## 8. 用户信息与账号 API

以下接口均需要鉴权（Bearer 或 Basic）。

### 8.1 获取用户信息

- 方法：`GET`
- 路径：`/api/v1/public/webdav/user/info`

响应示例：

```json
{
  "username": "alice",
  "wallet_address": "0x...",
  "permissions": ["create", "read", "update", "delete"],
  "created_at": "2024-01-01 12:00:00",
  "updated_at": "2024-01-02 12:00:00",
  "has_password": true
}
```

### 8.2 更新用户名

- 方法：`POST`
- 路径：`/api/v1/public/webdav/user/update`

Body：

```json
{ "username": "alice2" }
```

成功响应：

```json
{ "username": "alice2" }
```

错误响应（示例）：

```json
{ "error": "Username already exists", "code": 409, "success": false }
```

### 8.3 修改/设置密码

- 方法：`POST`
- 路径：`/api/v1/public/webdav/user/password`

Body：

```json
{ "oldPassword": "old", "newPassword": "newStrongPassword" }
```

说明：
- 如果用户已有密码，`oldPassword` 必填；否则可省略。

成功响应：

```json
{ "success": true }
```

错误响应（示例）：

```json
{ "error": "Old password is incorrect", "code": 401, "success": false }
```

### 8.4 管理员用户管理

需要管理员权限（`security.admin_addresses` 白名单中的钱包地址登录）。

- `GET /api/v1/public/admin/users/list`
- `POST /api/v1/public/admin/users/create`
- `POST /api/v1/public/admin/users/update`
- `POST /api/v1/public/admin/users/delete`
- `POST /api/v1/public/admin/users/reset-password`

创建用户示例：

```json
{
  "username": "alice",
  "password": "password123",
  "wallet_address": "0x...",
  "directory": "alice",
  "permissions": ["CRUD"],
  "quota": 5368709120,
  "rules": [
    { "path": "/private", "permissions": ["read"], "regex": false }
  ]
}
```

更新用户示例：

```json
{
  "username": "alice",
  "new_username": "alice2",
  "permissions": ["read", "update"],
  "quota": 10737418240
}
```

删除用户示例：

```json
{ "username": "alice" }
```

重置密码示例：

```json
{ "username": "alice", "password": "newStrongPassword" }
```

## 9. 地址簿 API

以下接口均需要鉴权（Bearer 或 Basic）。

### 9.1 分组

- `GET /api/v1/public/webdav/address/groups`
- `POST /api/v1/public/webdav/address/groups/create`
- `PUT /api/v1/public/webdav/address/groups/update`
- `DELETE /api/v1/public/webdav/address/groups/delete`

创建分组示例：

```bash
curl -X POST -u alice:password123 \
  -H "Content-Type: application/json" \
  -d '{"name":"合作伙伴"}' \
  http://127.0.0.1:6065/api/v1/public/webdav/address/groups/create
```

列表响应示例：

```json
{
  "items": [
    { "id": "g1", "name": "合作伙伴", "createdAt": "2024-01-01 12:00:00" }
  ]
}
```

更新/删除说明：
- `PUT /api/v1/public/webdav/address/groups/update` 与 `DELETE /api/v1/public/webdav/address/groups/delete` 成功时返回 `200`，通常无响应体。

### 9.2 联系人

- `GET /api/v1/public/webdav/address/contacts`
- `POST /api/v1/public/webdav/address/contacts/create`
- `PUT /api/v1/public/webdav/address/contacts/update`
- `DELETE /api/v1/public/webdav/address/contacts/delete`

创建联系人示例：

```bash
curl -X POST -u alice:password123 \
  -H "Content-Type: application/json" \
  -d '{"name":"Bob","walletAddress":"0x1234...","groupId":"<groupId>","tags":["team"]}' \
  http://127.0.0.1:6065/api/v1/public/webdav/address/contacts/create
```

列表响应示例：

```json
{
  "items": [
    {
      "id": "c1",
      "name": "Bob",
      "walletAddress": "0x1234...",
      "groupId": "g1",
      "tags": ["team"],
      "createdAt": "2024-01-01 12:00:00"
    }
  ]
}
```

更新联系人响应示例：

```json
{
  "id": "c1",
  "name": "Bob",
  "walletAddress": "0x1234...",
  "groupId": "g1",
  "tags": ["team"]
}
```

删除联系人说明：
- `DELETE /api/v1/public/webdav/address/contacts/delete` 成功时返回 `200`，通常无响应体。

## 10. 公开分享链接 API

创建/管理接口需要鉴权；访问分享链接为公开接口。

### 10.1 创建分享链接

- 方法：`POST`
- 路径：`/api/v1/public/share/create`

Body：

```json
{ "path": "/docs/file.txt", "expiresIn": 3600 }
```

成功响应：

```json
{
  "token": "share-token",
  "name": "file.txt",
  "path": "/docs/file.txt",
  "url": "http://127.0.0.1:6065/api/v1/public/share/share-token",
  "viewCount": 0,
  "downloadCount": 0,
  "expiresAt": "2024-01-01 12:00:00"
}
```

### 10.2 列表与撤销

- `GET /api/v1/public/share/list`
- `POST /api/v1/public/share/revoke`（Body：`{"token":"..."}`）

列表响应示例：

```json
{
  "items": [
    {
      "token": "share-token",
      "name": "file.txt",
      "path": "/docs/file.txt",
      "url": "http://127.0.0.1:6065/api/v1/public/share/share-token",
      "viewCount": 1,
      "downloadCount": 0,
      "expiresAt": "2024-01-01 12:00:00",
      "createdAt": "2024-01-01 10:00:00"
    }
  ]
}
```

撤销成功响应示例：

```json
{ "message": "revoked successfully" }
```

### 10.3 访问分享链接（公开）

- `GET /api/v1/public/share/{token}`
- `HEAD /api/v1/public/share/{token}`

说明：
- 该接口直接下载文件，无需鉴权。
- 分享过期返回 `410 Gone`。
- 响应会携带 `Content-Disposition`，用于下载文件名。

## 11. 定向分享 API（share/user）

以下接口均需要鉴权（Bearer 或 Basic）。

### 11.1 创建定向分享

- 方法：`POST`
- 路径：`/api/v1/public/share/user/create`

Body：

```json
{
  "path": "/docs",
  "targetAddress": "0x...",
  "permissions": ["read", "create", "update", "delete"],
  "expiresIn": 86400
}
```

说明：
- `permissions` 也可传单个 `"CRUD"` 字符串。

### 11.2 列表/撤销

- `GET /api/v1/public/share/user/list`（我分享的）
- `GET /api/v1/public/share/user/received`（分享给我的）
- `POST /api/v1/public/share/user/revoke`（Body：`{"id":"..."}`）

列表响应示例（我分享的）：

```json
{
  "items": [
    {
      "id": "share-id",
      "name": "docs",
      "path": "/docs",
      "isDir": true,
      "permissions": ["read", "update"],
      "targetWallet": "0x...",
      "expiresAt": "2024-01-02 12:00:00",
      "createdAt": "2024-01-01 12:00:00"
    }
  ]
}
```

列表响应示例（分享给我的）：

```json
{
  "items": [
    {
      "id": "share-id",
      "name": "docs",
      "path": "/docs",
      "isDir": true,
      "permissions": ["read"],
      "ownerWallet": "0x...",
      "ownerName": "alice",
      "expiresAt": "2024-01-02 12:00:00",
      "createdAt": "2024-01-01 12:00:00"
    }
  ]
}
```

撤销成功响应示例：

```json
{ "message": "revoked successfully" }
```

### 11.3 浏览与下载

- `GET /api/v1/public/share/user/entries?shareId=...&path=/`
- `GET /api/v1/public/share/user/download?shareId=...&path=/file.txt`

目录条目响应示例：

```json
{
  "items": [
    { "name": "file.txt", "path": "/file.txt", "isDir": false, "size": 12, "modified": "2024-01-01 12:00:00" },
    { "name": "sub", "path": "/sub/", "isDir": true, "size": 0, "modified": "2024-01-01 12:00:00" }
  ]
}
```

下载说明：
- 响应为文件内容。
- 头部包含 `Content-Disposition`。

### 11.4 上传与目录操作

- `PUT` 或 `POST` `/api/v1/public/share/user/upload?shareId=...&path=/file.txt`（`multipart/form-data`，字段 `file`）
- `POST /api/v1/public/share/user/folder`
- `POST /api/v1/public/share/user/rename`
- `DELETE /api/v1/public/share/user/item`

Body 示例（folder/rename/item）：

```json
{ "shareId": "share-id", "path": "/new-folder" }
```

```json
{ "shareId": "share-id", "from": "/a.txt", "to": "/b.txt" }
```

成功响应示例：

```json
{ "message": "uploaded successfully" }
```

```json
{ "message": "created successfully" }
```

```json
{ "message": "renamed successfully" }
```

```json
{ "message": "deleted successfully" }
```

## 12. 常见状态码

- `200/201/204`：成功
- `207 Multi-Status`：PROPFIND 成功（XML 响应）
- `401 Unauthorized`：未认证或 token 无效
- `403 Forbidden`：无权限
- `404 Not Found`：路径不存在
- `409 Conflict`：目录冲突或已存在
- `412 Precondition Failed`：条件不满足（如 Overwrite=F）
- `410 Gone`：分享链接已过期
- `507 Insufficient Storage`：配额不足

## 13. 注意事项

- 路径请使用 URL 编码（空格、中文等需要编码）。
- `Destination` 可以是完整 URL 或绝对路径。
- 目录列举使用 `PROPFIND`，响应为 XML（建议配合 `xq` 查看）。
- 系统会忽略 `._*`、`.DS_Store`、`.AppleDouble`、`Thumbs.db` 等系统文件。
- WebDAV 支持 Unicode 路径。

## 14. 权限规则（rules）

用户规则存储在数据库，可通过管理员接口更新。规则按顺序匹配，命中后不再继续匹配；未命中则回退到用户默认权限。

更新示例（管理员接口）：

```json
{
  "username": "alice",
  "rules": [
    { "path": "/private", "permissions": ["read"], "regex": false },
    { "path": "^/projects/.+/readonly(/|$)", "permissions": ["read"], "regex": true }
  ]
}
```

说明：
- `regex: false` 使用前缀匹配（`strings.HasPrefix`）。
- `regex: true` 使用 Go 正则表达式（`regexp`），路径会以 `/` 开头（例如 `/docs/file.txt`）。
- 正则建议显式写 `^` 和目录边界 `(/|$)`，避免误匹配。
