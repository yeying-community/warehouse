# Sharing & Recycle Design

This document covers public sharing, targeted sharing, and the recycle bin lifecycle.

## Public Sharing (Share)

### Create

```mermaid
sequenceDiagram
    participant C as Client
    participant H as ShareHandler
    participant S as ShareService
    participant R as ShareRepository
    participant FS as FileSystem

    C->>H: POST /api/v1/public/share/create
    H->>S: Create(path, expiresIn)
    S->>FS: Stat path
    alt path is dir
        S-->>H: error (directory not supported)
    end
    S->>R: Create share_items
    H-->>C: token + url
```

- Public shares support **files only** (directories are rejected).
- `expiresIn` adds an optional expiry time.

### Access

```mermaid
sequenceDiagram
    participant C as Client
    participant H as ShareHandler
    participant S as ShareService
    participant R as ShareRepository
    participant FS as FileSystem

    C->>H: GET /api/v1/public/share/{token}
    H->>S: Resolve(token)
    S->>R: GetByToken
    S->>FS: Open file
    H-->>C: ServeContent
    H->>S: IncrementView/Download (range-aware)
```

- Only the first range segment increments counters to avoid over-counting.

## Targeted Sharing (Share User)

### Creation & Permissions

- Target user is resolved by wallet address.
- Three audience modes are supported:
  - `targetMode=addresses`: address-sharing with one or more wallet addresses
  - `targetMode=groups`: address-book multi-group expansion via `groupIds` (snapshot to users)
  - `targetMode=all_users`: all signed-in users
- `permissions` can be `read/create/update/delete` (or `CRUD` string).
- Files or directories are supported.
- Successful create auto-adds the target to address book (if absent).

### Usage Example

```mermaid
sequenceDiagram
    participant C as Client
    participant H as ShareUserHandler
    participant S as ShareUserService
    participant R as UserShareRepository

    C->>H: GET /api/v1/public/share/user/entries?shareId=...&path=...
    H->>S: ResolveForTarget
    S->>R: GetByID
    H->>S: ResolveSharePath
    H-->>C: entries list
```

- Download, upload, create folder, rename, delete all require permission checks.

### Compatibility & Evolution

- API path remains unchanged (`share/user/*`), but create payload is upgraded to `targetMode + targetAddresses/groupIds`; legacy `targetAddress` payload is no longer supported.
- Backend persistence is now unified via `internal_share_items + internal_share_audiences`.
- Legacy `share_user_items` rows are idempotently backfilled into the new model during startup migration.

## Recycle Bin

### Write to Recycle (from WebDAV DELETE)

- WebDAV delete attempts to move the file into `.recycle`.
- A `recycle_items` record is created with hash/path/size/deleted time.

### Restore & Purge

- `recover`: move file back to original location; fails if target exists.
- `permanent`: delete recycle file and remove record.
- `clear`: batch clear all items.

### Naming Strategy

- New: `{hash}_{original name}`
- Legacy: `{username}_{directory}_{name}_{timestamp}`

## Relationship with Standby

- Sharing metadata (public share, internal share, audiences) lives in PostgreSQL and is naturally consistent across active/standby nodes using the same DB.
- File operations inside shared paths (upload/rename/delete) still go through the filesystem mutation path and existing internal replication chain.
- This sharing model evolution does not bypass the current standby replication design.
