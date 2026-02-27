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
- `asset-space-design.md`：登录后资产分层设计（个人资产 / 应用资产）
- `asset-space-implementation-checklist.md`：资产分层落地任务清单（实施步骤与验收）

## 相关补充

- 接口说明参考：`docs/webdav-api.md`
