#!/bin/sh
set -eu

ensure_writable_dir() {
  dir="$1"
  if [ -z "$dir" ]; then
    return
  fi
  mkdir -p "$dir"
  chown -R app:app "$dir"
}

case "${LOG_FILE_ENABLED:-true}" in
  false|FALSE|False|0)
    ;;
  *)
    ensure_writable_dir "${LOG_DIR:-./data/logs}"
    ;;
esac

exec su-exec app "$@"
