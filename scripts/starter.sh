#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BINARY="${ROOT_DIR}/bin/webdav"
CONFIG="${ROOT_DIR}/config.yaml"
PID_DIR="${ROOT_DIR}/run"
PID_FILE="${PID_DIR}/webdav.pid"
LOG_DIR="${ROOT_DIR}/logs"
LOG_FILE="${LOG_DIR}/webdav.log"

ACTION="${1:-start}"

if [[ ! -f "${CONFIG}" ]]; then
  CONFIG="${ROOT_DIR}/config.yaml.template"
fi

is_running() {
  if [[ -f "${PID_FILE}" ]]; then
    local pid
    pid=$(cat "${PID_FILE}")
    if [[ -n "${pid}" ]] && kill -0 "${pid}" >/dev/null 2>&1; then
      return 0
    fi
  fi
  return 1
}

start() {
  if [[ ! -x "${BINARY}" ]]; then
    echo "webdav binary not found: ${BINARY}" >&2
    exit 1
  fi
  if [[ ! -f "${CONFIG}" ]]; then
    echo "config not found: ${CONFIG}" >&2
    exit 1
  fi
  if is_running; then
    echo "webdav already running (pid=$(cat "${PID_FILE}"))"
    return
  fi

  mkdir -p "${PID_DIR}" "${LOG_DIR}"
  nohup "${BINARY}" -c "${CONFIG}" >>"${LOG_FILE}" 2>&1 &
  echo $! > "${PID_FILE}"

  sleep 1
  if ! is_running; then
    echo "failed to start webdav, check ${LOG_FILE}" >&2
    exit 1
  fi
  echo "webdav started (pid=$(cat "${PID_FILE}"))"
}

stop() {
  if ! is_running; then
    echo "webdav not running"
    rm -f "${PID_FILE}"
    return
  fi

  local pid
  pid=$(cat "${PID_FILE}")
  kill "${pid}" >/dev/null 2>&1 || true

  for _ in {1..20}; do
    if ! kill -0 "${pid}" >/dev/null 2>&1; then
      rm -f "${PID_FILE}"
      echo "webdav stopped"
      return
    fi
    sleep 0.5
  done

  echo "webdav did not stop in time, sending SIGKILL" >&2
  kill -9 "${pid}" >/dev/null 2>&1 || true
  rm -f "${PID_FILE}"
  echo "webdav stopped"
}

restart() {
  stop
  start
}

case "${ACTION}" in
  start)
    start
    ;;
  stop)
    stop
    ;;
  restart)
    restart
    ;;
  *)
    echo "usage: $0 [start|stop|restart]" >&2
    exit 1
    ;;
esac
