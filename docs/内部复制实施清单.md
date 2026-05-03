# Internal 复制实施清单

本文把“active -> standby 内部复制”方案拆成可执行的落地步骤，目标是形成一条能逐步提交代码的实施路径。

> 注意：本清单面向实施，不代表当前仓库已完成这些能力。

## 1. 阶段 0：节点身份与内部鉴权

### 任务

- 增加节点身份配置：
  - `node.id`
  - `node.role`（`active` / `standby`）
- 增加内部复制配置：
  - `replication.enabled`
  - `replication.shared_secret` 或 mTLS 配置
  - 重试 / 退避 / 超时参数：
    - `replication.dispatch_interval`
    - `replication.request_timeout`
    - `replication.batch_size`
    - `replication.retry_backoff_base`
    - `replication.max_retry_backoff`
- 增加 internal auth middleware
- 约束 standby 默认不暴露 public/admin 写流量

### 主要改动点

- [internal/infrastructure/config/config.go](../../internal/infrastructure/config/config.go)
- [internal/infrastructure/config/loader.go](../../internal/infrastructure/config/loader.go)
- [internal/interface/http/router.go](../../internal/interface/http/router.go)
- [internal/container/container.go](../../internal/container/container.go)

### 验收

- active / standby 都能加载节点身份配置
- 未携带合法内部签名的请求无法访问 internal 接口
- standby 的角色语义清晰可见

## 2. 阶段 1：Outbox 表与仓储

### 任务

- 新增 `replication_outbox`
- 新增 `replication_offsets`
- 编写 repository 接口与 PostgreSQL 实现
- 增加基础查询：
  - append event
  - list pending
  - mark dispatched / failed
  - update last applied
  - query lag

### 主要改动点

- [internal/infrastructure/database/postgres.go](../../internal/infrastructure/database/postgres.go)
- `internal/infrastructure/repository/*` 新增复制仓储
- [internal/container/container.go](../../internal/container/container.go)

### 验收

- 可以插入、查询、重试 outbox 事件
- 可以记录并读取 standby 的最后应用序号

## 3. 阶段 2：文件变更采集抽象

### 任务

- 定义统一的 `ReplicationRecorder` / `MutationRecorder`
- 统一使用 `webdav.directory` 根目录下的规范化相对路径
- 收拢各写路径，在本地文件操作成功后记录 outbox 事件

### 必须覆盖的写入口

- WebDAV：
  - [internal/application/service/webdav_service.go](../../internal/application/service/webdav_service.go)
- 回收站：
  - [internal/application/service/recycle_service.go](../../internal/application/service/recycle_service.go)
- 定向分享直接文件操作：
  - [internal/interface/http/handler/share_user.go](../../internal/interface/http/handler/share_user.go)

### 建议的第一版事件类型

- `ensure_dir`
- `upsert_file`
- `move_path`
- `copy_path`
- `remove_path`

### 验收

- 每个写入口都能产出对应事件
- 回收站操作不再是特殊复制逻辑，而是通用 `move/remove`
- 事件路径全部规范化且可重放

## 4. 阶段 3：Standby Internal Apply Handler

### 任务

- 新增 internal handler：
  - `fs/apply`
  - `file apply`
  - `status`
- 实现幂等 apply
- 对重复事件和重复写入保持稳定结果
- 对序号跳跃提供明确错误

### 主要改动点

- `internal/interface/http/handler/*` 新增 internal replication handler
- [internal/interface/http/router.go](../../internal/interface/http/router.go)
- [internal/container/container.go](../../internal/container/container.go)

### 验收

- 同一事件重复调用不会破坏文件树
- standby 能正确应用 mkdir / upload / move / delete / recycle 相关事件
- 状态接口能返回最后应用序号

## 5. 阶段 4：Active Worker 与重试

### 任务

- 新增复制 worker，只在 active 角色运行
- 串行发送到 standby，保证顺序
- 支持失败重试与指数退避
- 对 `upsert_file` 使用流式传输，不把内容塞进数据库

### 主要改动点

- `internal/application/...` 或 `internal/infrastructure/...` 新增 replication worker/client
- [internal/container/container.go](../../internal/container/container.go)

### 验收

- standby 临时不可用时，outbox 会积压并重试
- standby 恢复后能继续追平
- 大文件复制不会把 PostgreSQL 当作内容存储

## 6. 阶段 5：首次全量同步与 Reconcile

### 任务

- 明确首次全量同步流程
- 增加基线标记能力
- 增加周期性 reconcile 机制
- 对漂移提供修复或报警

### 说明

第一版不建议从空盘直接依赖增量事件。

建议：

1. 先做一次离线全量同步
2. 记录基线
3. 再开启增量复制

### 验收

- 新 standby 可以通过“全量 + 增量”接入
- reconcile 可以发现并修复典型漏同步场景

## 7. 阶段 6：状态、告警与切换 SOP

### 任务

- 暴露复制状态接口
- 增加日志和指标：
  - pending 数量
  - oldest pending age
  - last applied seq
  - retry count
  - last error
- 编写切换 SOP
- 编写演练步骤

### 验收

- 切换前可以明确判断 standby 是否满足接管条件
- 运维可以通过状态接口而不是“猜测”来判断 lag
- 完成至少一次演练并记录 RPO / RTO

## 8. 建议的提交顺序

建议按下面顺序拆 PR：

1. 配置与内部鉴权骨架
2. Outbox 表与 repository
3. Internal handler 与 status 接口
4. Worker 与基础重试
5. WebDAV 写路径接入复制事件
6. 回收站接入复制事件
7. 定向分享文件操作接入复制事件
8. 全量同步 / reconcile / 切换 SOP

## 9. 首批最值得先做的代码点

如果现在开始写第一批代码，我建议优先做这几件事：

1. 配置模型扩展 + internal auth middleware
2. `replication_outbox` / `replication_offsets` 两张表
3. `GET /api/v1/internal/replication/status`
4. `ReplicationRecorder` 抽象

先把骨架搭出来，再逐步把写路径接进去，风险最小。
