# 数据模型

本文档概述 PostgreSQL 数据表结构与核心字段。

## ER 图

```mermaid
erDiagram
    USERS ||--o{ USER_RULES : has
    USERS ||--o{ RECYCLE_ITEMS : owns
    USERS ||--o{ SHARE_ITEMS : shares
    USERS ||--o{ ADDRESS_GROUPS : groups
    USERS ||--o{ ADDRESS_CONTACTS : contacts
    ADDRESS_GROUPS ||--o{ ADDRESS_CONTACTS : contains

    USERS ||--o{ SHARE_USER_ITEMS : owner
    USERS ||--o{ SHARE_USER_ITEMS : target

    USERS {
        string id PK
        string username
        string password
        string wallet_address
        string email
        string directory
        string permissions
        int quota
        int used_space
        datetime created_at
        datetime updated_at
    }

    USER_RULES {
        int id PK
        string user_id FK
        string path
        string permissions
        bool regex
        datetime created_at
    }

    RECYCLE_ITEMS {
        string id PK
        string hash
        string user_id FK
        string username
        string directory
        string name
        string path
        int size
        datetime deleted_at
        datetime created_at
    }

    SHARE_ITEMS {
        string id PK
        string token
        string user_id FK
        string username
        string name
        string path
        datetime expires_at
        int view_count
        int download_count
        datetime created_at
    }

    SHARE_USER_ITEMS {
        string id PK
        string owner_user_id FK
        string owner_username
        string target_user_id FK
        string target_wallet_address
        string name
        string path
        bool is_dir
        string permissions
        datetime expires_at
        datetime created_at
    }

    ADDRESS_GROUPS {
        string id PK
        string user_id FK
        string name
        datetime created_at
    }

    ADDRESS_CONTACTS {
        string id PK
        string user_id FK
        string group_id FK
        string name
        string wallet_address
        string[] tags
        datetime created_at
    }
```

## 关键表说明

- **users**：用户主表，包含权限、配额与钱包地址。
- **user_rules**：路径级权限规则，优先于默认权限。
- **recycle_items**：回收站记录，用于恢复或永久删除。
- **share_items**：公开分享记录，按 token 访问。
- **share_user_items**：定向分享记录（指定 target 用户）。
- **address_groups / address_contacts**：地址簿与联系人分组。

## 重要索引/约束（摘要）

- `users.username` 唯一
- `users.wallet_address` 唯一（非空时）
- `users.email` 唯一（非空时）
- `share_items.token` 唯一
- `recycle_items.hash` 唯一
- `address_groups(user_id, name)` 唯一
- `address_contacts(user_id, wallet_address)` 唯一
