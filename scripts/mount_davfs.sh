#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  # Mount
  scripts/mount_davfs.sh mount <url> <mount_point> <username> [password]

  # Unmount
  scripts/mount_davfs.sh umount <mount_point>

  # Configure /etc/fstab for auto mount at boot
  scripts/mount_davfs.sh install-fstab <url> <mount_point> <username> [password]

  # Remove /etc/fstab auto mount entry
  scripts/mount_davfs.sh remove-fstab <mount_point>

Notes:
  - If password is omitted, it will be read from terminal securely.
  - Credentials are written to /etc/davfs2/secrets (chmod 600).
  - Requires davfs2 and sudo permissions.
EOF
}

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Missing required command: $1" >&2
    exit 1
  fi
}

upsert_secret() {
  local url="$1"
  local username="$2"
  local password="$3"
  local secrets_file="/etc/davfs2/secrets"
  local tmp_file

  tmp_file="$(mktemp)"
  trap 'rm -f "${tmp_file:-}"' RETURN

  if sudo test -f "$secrets_file"; then
    sudo awk -v url="$url" -v user="$username" -v pass="$password" '
      $1 == url {
        if (!done) {
          print url " " user " " pass
          done = 1
        }
        next
      }
      { print }
      END {
        if (!done) {
          print url " " user " " pass
        }
      }
    ' "$secrets_file" >"$tmp_file"
  else
    printf '%s %s %s\n' "$url" "$username" "$password" >"$tmp_file"
  fi

  sudo install -D -m 600 "$tmp_file" "$secrets_file"
  trap - RETURN
  rm -f "$tmp_file"
}

escape_fstab_field() {
  local value="$1"
  value="${value//\\/\\\\}"
  value="${value// /\\040}"
  value="${value//$'\t'/\\011}"
  printf '%s' "$value"
}

upsert_fstab_entry() {
  local url="$1"
  local mount_point="$2"
  local fstab_file="/etc/fstab"
  local escaped_url escaped_mount entry uid gid tmp_file

  uid="$(id -u)"
  gid="$(id -g)"
  escaped_url="$(escape_fstab_field "$url")"
  escaped_mount="$(escape_fstab_field "$mount_point")"
  entry="${escaped_url} ${escaped_mount} davfs _netdev,nofail,uid=${uid},gid=${gid},file_mode=0644,dir_mode=0755 0 0"
  tmp_file="$(mktemp)"
  trap 'rm -f "${tmp_file:-}"' RETURN

  sudo awk -v mp="$escaped_mount" -v newline="$entry" '
    BEGIN { updated = 0 }
    /^[[:space:]]*#/ { print; next }
    NF >= 2 && $2 == mp {
      if (!updated) {
        print newline
        updated = 1
      }
      next
    }
    { print }
    END {
      if (!updated) {
        print newline
      }
    }
  ' "$fstab_file" >"$tmp_file"

  sudo install -m 644 "$tmp_file" "$fstab_file"
  trap - RETURN
  rm -f "$tmp_file"
}

remove_fstab_entry() {
  local mount_point="$1"
  local fstab_file="/etc/fstab"
  local escaped_mount tmp_file

  escaped_mount="$(escape_fstab_field "$mount_point")"
  tmp_file="$(mktemp)"
  trap 'rm -f "${tmp_file:-}"' RETURN

  sudo awk -v mp="$escaped_mount" '
    /^[[:space:]]*#/ { print; next }
    NF >= 2 && $2 == mp { next }
    { print }
  ' "$fstab_file" >"$tmp_file"

  sudo install -m 644 "$tmp_file" "$fstab_file"
  trap - RETURN
  rm -f "$tmp_file"
}

do_mount() {
  local url="$1"
  local mount_point="$2"
  local username="$3"
  local password="${4:-}"

  if [[ -z "$password" ]]; then
    read -r -s -p "Password for ${username}: " password
    echo
  fi

  if mountpoint -q "$mount_point"; then
    echo "Already mounted: $mount_point"
    return 0
  fi

  sudo mkdir -p "$mount_point"
  upsert_secret "$url" "$username" "$password"

  sudo mount -t davfs "$url" "$mount_point" \
    -o "uid=$(id -u),gid=$(id -g),file_mode=0644,dir_mode=0755"

  if mountpoint -q "$mount_point"; then
    echo "Mounted: $url -> $mount_point"
    return 0
  fi

  echo "Mount failed: $url -> $mount_point" >&2
  return 1
}

do_umount() {
  local mount_point="$1"
  if ! mountpoint -q "$mount_point"; then
    echo "Not mounted: $mount_point"
    return 0
  fi

  sudo umount "$mount_point"
  echo "Unmounted: $mount_point"
}

do_install_fstab() {
  local url="$1"
  local mount_point="$2"
  local username="$3"
  local password="${4:-}"

  if [[ -z "$password" ]]; then
    read -r -s -p "Password for ${username}: " password
    echo
  fi

  sudo mkdir -p "$mount_point"
  upsert_secret "$url" "$username" "$password"
  upsert_fstab_entry "$url" "$mount_point"

  if command -v systemctl >/dev/null 2>&1; then
    sudo systemctl daemon-reload || true
  fi

  if mountpoint -q "$mount_point"; then
    echo "Already mounted: $mount_point"
  else
    sudo mount "$mount_point" || true
  fi

  echo "fstab configured: $url -> $mount_point"
}

do_remove_fstab() {
  local mount_point="$1"
  remove_fstab_entry "$mount_point"

  if command -v systemctl >/dev/null 2>&1; then
    sudo systemctl daemon-reload || true
  fi

  echo "fstab entry removed for: $mount_point"
}

main() {
  local action="${1:-mount}"

  case "$action" in
    -h|--help|help)
      usage
      return 0
      ;;
  esac

  need_cmd mountpoint
  need_cmd sudo
  need_cmd awk

  if ! command -v mount.davfs >/dev/null 2>&1; then
    echo "mount.davfs not found. Please install davfs2 first." >&2
    exit 1
  fi

  case "$action" in
    mount)
      if [[ $# -lt 4 || $# -gt 5 ]]; then
        usage
        exit 1
      fi
      do_mount "$2" "$3" "$4" "${5:-}"
      ;;
    umount|unmount)
      if [[ $# -ne 2 ]]; then
        usage
        exit 1
      fi
      do_umount "$2"
      ;;
    install-fstab)
      if [[ $# -lt 4 || $# -gt 5 ]]; then
        usage
        exit 1
      fi
      do_install_fstab "$2" "$3" "$4" "${5:-}"
      ;;
    remove-fstab)
      if [[ $# -ne 2 ]]; then
        usage
        exit 1
      fi
      do_remove_fstab "$2"
      ;;
    -h|--help|help)
      usage
      ;;
    *)
      usage
      exit 1
      ;;
  esac
}

main "$@"
