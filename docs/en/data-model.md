# Data Model

This document summarizes the PostgreSQL schema and key relationships.

## ER Diagram

```mermaid
erDiagram
    USERS ||--o{ USER_RULES : has
    USERS ||--o{ RECYCLE_ITEMS : owns
    USERS ||--o{ SHARE_ITEMS : shares
    USERS ||--o{ ADDRESS_GROUPS : groups
    USERS ||--o{ ADDRESS_CONTACTS : contacts
    ADDRESS_GROUPS ||--o{ ADDRESS_CONTACTS : contains

    USERS ||--o{ SHARE_USER_ITEMS : owner_legacy
    USERS ||--o{ SHARE_USER_ITEMS : target_legacy
    USERS ||--o{ INTERNAL_SHARE_ITEMS : owner
    INTERNAL_SHARE_ITEMS ||--o{ INTERNAL_SHARE_AUDIENCES : has
    USERS ||--o{ INTERNAL_SHARE_AUDIENCES : target_user

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

    INTERNAL_SHARE_ITEMS {
        string id PK
        string owner_user_id FK
        string owner_username
        string name
        string path
        bool is_dir
        string permissions
        datetime expires_at
        string status
        datetime created_at
        datetime updated_at
    }

    INTERNAL_SHARE_AUDIENCES {
        string id PK
        string share_id FK
        string audience_type
        string target_user_id FK
        string target_wallet_address
        string source_group_id FK
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

## Key Tables

- **users**: core user record with permissions, quota, and wallet address.
- **user_rules**: path-level rules that override default permissions.
- **recycle_items**: deleted file records for restore/permanent delete.
- **share_items**: public share records keyed by token.
- **share_user_items**: legacy targeted-share table kept for backward compatibility.
- **internal_share_items / internal_share_audiences**: internal-share canonical tables for single-user, group-expanded, and all-users audiences.
- **address_groups / address_contacts**: address book and contacts.

## Migration & Compatibility

- Startup migration idempotently backfills legacy `share_user_items` rows into `internal_share_items / internal_share_audiences`.
- New writes are persisted to `internal_share_*`; legacy table remains mainly for compatibility and rollback fallback.
- API path remains unchanged (`/api/v1/public/share/user/*`), but create payload has been upgraded to `targetMode + targetAddresses/groupIds`; legacy single-field `targetAddress` payload is no longer supported.

## Indexes & Constraints (summary)

- `users.username` unique
- `users.wallet_address` unique (when non-null)
- `users.email` unique (when non-null)
- `share_items.token` unique
- `internal_share_items.id` unique
- `internal_share_audiences.id` unique
- `internal_share_audiences(share_id, audience_type, target_user_id)` unique when `audience_type='user'`
- `internal_share_audiences(share_id, audience_type)` unique when `audience_type='all_users'`
- `recycle_items.hash` unique
- `address_groups(user_id, name)` unique
- `address_contacts(user_id, wallet_address)` unique
