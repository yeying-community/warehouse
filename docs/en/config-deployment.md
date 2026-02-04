# Configuration & Deployment

This document explains config loading/overrides and common deployment patterns.

## Config Sources & Priority

Loading order:

1. **Defaults**: `config.DefaultConfig()`
2. **Config file**: YAML via `-c/--config`
3. **CLI flags**: override a subset of fields (addr/port/TLS/dir, etc.)
4. **Environment variables**: override select fields (e.g. `WEBDAV_JWT_SECRET`)

> Later sources override earlier ones.

## Validation Highlights

Startup validation fails fast on:

- `web3.jwt_secret` required and at least 32 chars
- `database.type` must be `postgres` / `postgresql`
- `webdav.directory` must exist or be creatable
- TLS requires `cert_file` / `key_file`

## Key Config Blocks

- `server`: address, port, TLS, timeouts
- `database`: PostgreSQL connection + pool
- `webdav`: root directory, prefix, NoSniff
- `web3`: JWT secret, token TTLs, UCAN rules
- `security`: no-password mode, reverse proxy flag, admin allowlist
- `cors`: CORS settings

## Override Examples

```bash
# 1) Start with config file
webdav -c config.yaml

# 2) Override port/dir via flags
webdav -c config.yaml -p 8080 -d /data

# 3) Override JWT secret via env
export WEBDAV_JWT_SECRET="your-secret"
webdav -c config.yaml
```

## Deployment Options

### Direct Binary

- Build via `make`, run `build/webdav`
- Use systemd/supervisor for process management

### Container (Docker / Compose)

- `Dockerfile` and `docker-compose.yml` are provided
- Key points:
  - mount `config.yaml`
  - mount the data directory (`webdav.directory`)
  - ensure PostgreSQL connectivity

### Reverse Proxy

- When using Nginx/Traefik, set `security.behind_proxy=true`
- If TLS terminates at proxy, pass `X-Forwarded-Proto`

## Persistence

- File data: stored under `webdav.directory` (use external volume)
- Metadata: PostgreSQL tables (users/share/recycle/address book)

## Health Check

- `GET /api/v1/public/health/heartbeat`
- WebDAV access via Basic or Bearer
