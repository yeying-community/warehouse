# WebDAV 设计文档索引

本目录包含基于当前代码实现整理的设计文档，面向开发/维护人员。

快速开始：`README.md`

## 文档列表

- `architecture.md`：总体架构与启动/请求处理链路
- `authentication.md`：认证体系（Basic/Web3/JWT/UCAN）与登录流程
- `ucan.md`：WebDAV 中 UCAN 校验与能力匹配规则
- `config-deployment.md`：配置加载/覆盖规则与部署方式
- `webdav-flow.md`：WebDAV 请求处理流程与配额/权限/回收站
- `data-model.md`：数据库表结构与关系
- `share-recycle.md`：分享与回收站设计
- `share-evolution-plan.md`：目录共享 / 分组共享 / 全员共享的统一演进方案
- `multi-instance-replica-design.md`：多实例 / 多副本部署方案与演进建议
- `ha-active-standby-deployment.md`：阶段一高可用部署（单活 + 多 standby）落地指南
- `internal-replication-design.md`：阶段一 `internal` 复制版 standby / 多 standby 设计
- `internal-replication-implementation-checklist.md`：阶段一 `internal` 复制实施清单
- `../容灾方案.md`：当前容灾路线、standby QA、已实现能力与待办
- `asset-space-design.md`：登录后资产分层设计（个人资产 / 应用资产）
- `asset-space-implementation-checklist.md`：资产分层落地任务清单（实施步骤与验收）

## 相关补充

- 接口说明参考：`docs/webdav-api.md`
