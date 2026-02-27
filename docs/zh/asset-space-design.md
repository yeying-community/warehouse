# 资产空间分层设计（个人资产 / 应用资产）

本文档定义登录后资产管理界面的信息架构与目录规范，目标是把用户资产分成两类并独立管理：

- 个人资产（用户主动上传/管理）
- 应用资产（第三方应用写入，按 appId 隔离）

## 1. 设计目标

1. 登录后提供两个明确入口，避免“个人文件”和“应用文件”混在同一层。
2. 保持 WebDAV 协议兼容，Finder/Windows/rclone 等第三方客户端可直接访问。
3. 与 UCAN app scope 对齐，确保第三方 DApp 仅可访问自己的应用目录。
4. 对现有数据和现有客户端保持平滑迁移。

## 2. 目录命名与结构

## 2.1 命名结论

个人资产目录命名为：`personal`  
应用资产根目录命名为：`apps`（保持现有约定）

命名理由：

- `personal` 语义直接，便于前后端、文档和第三方工具统一理解。
- 不使用中文目录名，避免不同客户端编码/展示差异。
- 与 `apps` 平行，结构清晰，便于后续扩展（如 `shared` / `archive`）。

## 2.2 用户根目录结构

每个用户注册成功后，初始化其独立根目录：

```text
/<user_root>/
  personal/                 # 个人资产空间（默认入口）
  apps/                     # 应用资产空间（应用隔离根）
    <appId-a>/
    <appId-b>/
```

说明：

- `apps/<appId>/` 由 DApp 首次写入时按需创建（或由服务端预创建）。
- `appId` 推荐使用域名主机部分，如 `dapp.example.com`。

## 3. 登录后 UI 入口设计

建议在登录后的主导航中，明确提供两个一级入口：

- `个人资产` -> 对应路径 `/personal`
- `应用资产` -> 对应路径 `/apps`

交互建议：

1. 登录成功默认进入 `个人资产`（`/personal`），符合普通用户心智。
2. 切换到 `应用资产` 后先展示应用列表（`/apps` 下一级目录），点击进入某个 app 子目录。
3. 面包屑中显式显示空间类型，例如：`个人资产 / 合同 / xx.pdf`、`应用资产 / dapp.example.com / data.json`。

## 4. 权限模型与隔离边界

## 4.1 个人资产访问

- 用户通过密码/JWT 登录后，可按其自身权限访问 `personal` 与其他允许路径。
- UI 层默认把日常上传、移动、删除操作落在 `personal`。

## 4.2 应用资产访问（UCAN）

UCAN 方式建议继续使用 app scope：

- 服务端配置：`required_resource=app:*`，`required_action=read,write`（或只读 `read`）。
- DApp cap：`resource=app:<appId>`。
- 请求路径必须在 `/apps/<appId>/...`。

这样可以保证：

- DApp A 不能访问 DApp B 的目录。
- DApp 默认也不能访问 `personal`（除非另行定义并显式授权）。

## 5. WebDAV 第三方客户端兼容性

第三方 WebDAV 客户端挂载后看到用户根目录下的 `personal/` 与 `apps/` 两个主目录即可，无需协议改造。

推荐实践：

1. 对普通用户文档提示“日常文件放 `personal/`”。
2. 对开发者文档提示“应用数据仅写入 `apps/<appId>/`”。
3. 保持根路径可访问，避免强制路径重写导致客户端 301/404 问题。

## 6. 注册初始化与迁移策略

## 6.1 新用户

注册成功后自动创建：

- `/<user_root>/personal`
- `/<user_root>/apps`

## 6.2 存量用户迁移

建议分阶段迁移：

1. 创建缺失目录：`personal/`、`apps/`。
2. 把用户根目录下的“业务文件”迁移到 `personal/`。
3. 保留 `apps/` 原有结构不变。
4. 提供一次性迁移日志与回滚窗口。

迁移白名单（不移动）建议包含：

- `apps`
- 系统回收/元数据目录（按现网实现）

## 7. API 与前端改造建议（最小增量）

为降低前端硬编码路径成本，建议新增或统一一个“资产空间元信息”接口，例如：

- `GET /api/v1/public/assets/spaces`

示例响应：

```json
{
  "code": 0,
  "data": {
    "defaultSpace": "personal",
    "spaces": [
      { "key": "personal", "name": "个人资产", "path": "/personal" },
      { "key": "apps", "name": "应用资产", "path": "/apps" }
    ]
  }
}
```

前端可基于该响应渲染入口，减少后续命名调整影响。

## 8. 推荐落地顺序

1. 先定目录规范：`personal` + `apps`。
2. 后端补注册初始化与存量迁移工具。
3. 前端增加“个人资产 / 应用资产”双入口。
4. 文档同步（API、UCAN、部署文档）。

---

相关文档：

- `ucan.md`：UCAN app scope 与 `app:<appId>` 规则
- `webdav-flow.md`：请求处理与权限链路
- `docs/webdav-api.md`：接口说明
- `asset-space-implementation-checklist.md`：实施任务清单与验收项
