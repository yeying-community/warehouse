# WebDAV 请求处理流程

本文档只描述“单次 WebDAV 请求进入服务端后，会经过哪些校验与文件系统操作”。

## 文档边界

以下内容不在本文重复展开：

- 服务整体模块划分、容器初始化、路由装配：见 [架构概览.md](./架构概览.md)
- 认证方式、UCAN、访问密钥、管理员能力：见 [认证设计.md](./认证设计.md)
- 回收站对外 API、分享接口：见 [WebDAV文件CRUD API（简明版）.md](./WebDAV文件CRUD API（简明版）.md)


## 处理链路总览

```mermaid
sequenceDiagram
    participant C as Client
    participant R as Router
    participant A as AuthMiddleware
    participant H as WebDAVHandler
    participant S as WebDAVService
    participant P as PermissionChecker
    participant Q as QuotaService
    participant W as webdav.Handler
    participant F as UnicodeFileSystem

    C->>R: WebDAV Request
    R->>A: Authenticate
    A-->>R: user
    R->>H: Handle
    H->>S: ServeHTTP
    S->>P: Check permission
    P-->>S: ok/deny
    alt Upload methods
        S->>Q: CheckQuota
        Q-->>S: ok/deny
    end
    S->>W: ServeHTTP
    W->>F: FS operations
    F-->>W: result
    W-->>S: status
    S-->>C: response
```

## 关键步骤说明

1. **认证**：通过 `AuthMiddleware` 获取用户信息，未授权直接拒绝。
2. **忽略系统文件**：对 `.DS_Store` / `.AppleDouble` / `Thumbs.db` / `._*` 等特殊路径返回 404/204。
3. **用户目录解析**：
   - `user.Directory` 为绝对路径时直接使用
   - 否则拼接为 `webdav.directory + user.Directory`
   - 若未设置 `user.Directory`，回退到 `webdav.directory`
   - 权限校验时使用 `user.Directory` 或 `user.Username` 作为逻辑前缀来组装路径
4. **权限校验**：将 HTTP 方法映射为权限操作（C/R/U/D），使用用户规则或默认权限判断。
5. **配额校验**：对 `PUT/POST/MKCOL` 检查追加大小是否超出 `user.quota`。
6. **WebDAV 处理**：
   - 使用自定义 `UnicodeFileSystem`，确保 Unicode 路径正确处理
   - 使用内存锁 `webdav.NewMemLS()`
7. **删除行为**：`DELETE` 默认移动到回收站目录 `.recycle` 并记录数据库。
8. **用量刷新**：对写操作成功后重新计算 `used_space` 并持久化。

## WebDAV 方法与权限映射

- `GET/HEAD/OPTIONS/PROPFIND` → Read (`R`)
- `PUT` → 目标不存在时 Create (`C`)，目标已存在时 Write (`U`)
- `PATCH/PROPPATCH` → Write (`U`)
- `POST/MKCOL` → Create (`C`)
- `COPY/MOVE` → Write (`U`)
- `DELETE` → Delete (`D`)
- 其他方法默认映射为 Read

权限匹配逻辑：

1. 若路径命中 `user_rules`，使用规则权限。
2. 否则使用 `users.permissions` 默认权限。

更完整的权限模型与认证边界，见 [认证设计.md](./认证设计.md)。

## DELETE 回收站流程

```mermaid
sequenceDiagram
    participant C as Client
    participant S as WebDAVService
    participant R as RecycleRepository
    participant FS as FileSystem

    C->>S: DELETE /path/to/file
    S->>FS: os.Stat
    S->>FS: os.Rename to .recycle/{hash}_{name}
    S->>R: Create recycle_items
    S-->>C: 200 OK
```

- 若移动失败，会回退为直接删除。
- 回收站文件命名规则：`{hash}_{原文件名}`。

## MOVE/COPY 目的路径规范化

对 `Destination` Header 做解码和规范化，避免代理或编码导致的路径异常。
