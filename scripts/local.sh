#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BASE_CONFIG_PATH="${ROOT_DIR}/config.yaml"
TMP_DIR="${ROOT_DIR}/.tmp"

ROLE="${1:-active}"

ACTIVE_PORT="${ACTIVE_PORT:-6065}"
STANDBY_PORT="${STANDBY_PORT:-6066}"
INTERNAL_SHARED_SECRET="${INTERNAL_SHARED_SECRET:-local-dev-internal-shared-secret}"

info() { printf '%s\n' "$*"; }
err() { printf '%s\n' "$*" >&2; }

usage() {
  cat <<'EOF'
用法：
  bash scripts/local.sh
  bash scripts/local.sh active
  bash scripts/local.sh standby

默认行为：
  - 不传参数时启动 active
  - 只使用现有 config.yaml 作为基础配置，不读取 config.yaml.template
  - 生成本地调试配置到 .tmp/active.yaml 或 .tmp/standby.yaml
  - 数据目录分别使用 .tmp/active/data 和 .tmp/standby/data
  - 使用 go run ./cmd/warehouse -c <generated-config> 前台启动

前提：
  - 需要先手动准备好 config.yaml
  - 需要先保证 go run ./cmd/warehouse -c config.yaml 可以正常启动

可选环境变量：
  ACTIVE_PORT / STANDBY_PORT
  INTERNAL_SHARED_SECRET
EOF
}

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    err "缺少依赖命令: $1"
    exit 1
  fi
}

render_config() {
  local role="$1"
  local output_path="$2"
  local node_id node_role port advertise_url peer_node_id peer_base_url data_dir

  case "$role" in
    active)
      node_id="warehouse-active"
      node_role="active"
      port="${ACTIVE_PORT}"
      advertise_url="http://127.0.0.1:${ACTIVE_PORT}"
      peer_node_id="warehouse-standby"
      peer_base_url=""
      data_dir="${TMP_DIR}/active/data"
      ;;
    standby)
      node_id="warehouse-standby"
      node_role="standby"
      port="${STANDBY_PORT}"
      advertise_url="http://127.0.0.1:${STANDBY_PORT}"
      peer_node_id="warehouse-active"
      peer_base_url=""
      data_dir="${TMP_DIR}/standby/data"
      ;;
    *)
      err "不支持的角色: ${role}"
      usage
      exit 1
      ;;
  esac

  cp "${BASE_CONFIG_PATH}" "${output_path}"

  # 只覆盖本地双实例必需字段，其余数据库、JWT、邮件等配置沿用现有 config.yaml。
  awk \
    -v port="${port}" \
    -v node_id="${node_id}" \
    -v node_role="${node_role}" \
    -v advertise_url="${advertise_url}" \
    -v peer_node_id="${peer_node_id}" \
    -v peer_base_url="${peer_base_url}" \
    -v shared_secret="${INTERNAL_SHARED_SECRET}" \
    -v data_dir="${data_dir}" \
    '
    BEGIN {
      section = ""
      subsection = ""
    }
    /^[A-Za-z0-9_-]+:$/ {
      section = ""
      subsection = ""
    }
    /^server:$/ {
      section = "server"
      subsection = ""
      print
      next
    }
    /^node:$/ {
      section = "node"
      subsection = ""
      print
      next
    }
    /^internal:$/ {
      section = "internal"
      subsection = ""
      print
      next
    }
    /^  replication:$/ && section == "internal" {
      subsection = "replication"
      print
      next
    }
    /^webdav:$/ {
      section = "webdav"
      subsection = ""
      print
      next
    }
    section == "server" && $1 == "address:" {
      print "  address: \"127.0.0.1\""
      next
    }
    section == "server" && $1 == "port:" {
      print "  port: " port
      next
    }
    section == "node" && $1 == "id:" {
      print "  id: \"" node_id "\""
      next
    }
    section == "node" && $1 == "role:" {
      print "  role: \"" node_role "\""
      next
    }
    section == "node" && $1 == "advertise_url:" {
      print "  advertise_url: \"" advertise_url "\""
      next
    }
    section == "internal" && subsection == "replication" && $1 == "enabled:" {
      print "    enabled: true"
      next
    }
    section == "internal" && subsection == "replication" && $1 == "peer_node_id:" {
      print "    peer_node_id: \"" peer_node_id "\""
      next
    }
    section == "internal" && subsection == "replication" && $1 == "peer_base_url:" {
      print "    peer_base_url: \"" peer_base_url "\""
      next
    }
    section == "internal" && subsection == "replication" && $1 == "shared_secret:" {
      print "    shared_secret: \"" shared_secret "\""
      next
    }
    section == "internal" && subsection == "replication" && $1 == "dispatch_interval:" {
      print "    dispatch_interval: 1s"
      next
    }
    section == "internal" && subsection == "replication" && $1 == "request_timeout:" {
      print "    request_timeout: 10s"
      next
    }
    section == "internal" && subsection == "replication" && $1 == "retry_backoff_base:" {
      print "    retry_backoff_base: 1s"
      next
    }
    section == "internal" && subsection == "replication" && $1 == "max_retry_backoff:" {
      print "    max_retry_backoff: 10s"
      next
    }
    section == "webdav" && $1 == "directory:" {
      print "  directory: \"" data_dir "\""
      next
    }
    {
      print
    }
    ' "${output_path}" > "${output_path}.tmp"

  mv "${output_path}.tmp" "${output_path}"

  if ! grep -q '^  advertise_url:' "${output_path}"; then
    awk \
      -v advertise_url="${advertise_url}" \
      '
      /^node:$/ {
        in_node = 1
        print
        next
      }
      in_node && /^  role:/ {
        print
        print "  advertise_url: \"" advertise_url "\""
        in_node = 0
        next
      }
      {
        print
      }
      ' "${output_path}" > "${output_path}.tmp"
    mv "${output_path}.tmp" "${output_path}"
  fi
}

main() {
  case "${ROLE}" in
    active|"")
      ROLE="active"
      ;;
    standby)
      ;;
    -h|--help|help)
      usage
      exit 0
      ;;
    *)
      err "不支持的参数: ${ROLE}"
      usage
      exit 1
      ;;
  esac

  require_command go
  require_command awk

  if [[ ! -f "${BASE_CONFIG_PATH}" ]]; then
    err "缺少基础配置文件: ${BASE_CONFIG_PATH}"
    err "请先手动从 config.yaml.template 复制出 config.yaml，并确保单实例可以正常启动。"
    exit 1
  fi

  mkdir -p "${TMP_DIR}/active/data" "${TMP_DIR}/standby/data"

  local config_path
  config_path="${TMP_DIR}/${ROLE}.yaml"
  render_config "${ROLE}" "${config_path}"

  info "role=${ROLE}"
  info "base_config=${BASE_CONFIG_PATH}"
  info "generated_config=${config_path}"
  info "data_dir=${TMP_DIR}/${ROLE}/data"
  info "command=go run ./cmd/warehouse -c ${config_path}"

  cd "${ROOT_DIR}"
  exec go run ./cmd/warehouse -c "${config_path}"
}

main "$@"
