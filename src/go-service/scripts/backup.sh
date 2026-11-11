#!/usr/bin/env bash
# Stop every writer first; this script does not stop services automatically.
set -euo pipefail
umask 077
if [[ "${1:-}" != --confirm-stopped || $# -ne 3 ]]; then
  echo 'Usage: backup.sh --confirm-stopped DATA_ROOT NEW_BACKUP_DIRECTORY' >&2; exit 2
fi
data_root="$(cd -- "$2" && pwd)"
backup_dir="$3"
if [[ -e "$backup_dir" ]]; then echo 'Backup destination must be new' >&2; exit 2; fi
mkdir -p -- "$backup_dir"
backup_dir="$(cd -- "$backup_dir" && pwd)"
case "$backup_dir/" in "$data_root/"*) echo 'Backup destination must be outside data root' >&2; exit 2;; esac
printf 'incomplete\n' > "$backup_dir/STATUS"
database_url="${GO_SERVICE_DATABASE_URL:-${DATABASE_URL:-}}"
if [[ -n "$database_url" ]]; then
  command -v pg_dump >/dev/null
  PGDATABASE="$database_url" pg_dump --format=custom --file="$backup_dir/postgres.dump"
fi
tar -czf "$backup_dir/data.tar.gz" -C "$data_root" .
printf 'complete\n' > "$backup_dir/STATUS"
printf 'Backup complete: %s\n' "$backup_dir"
