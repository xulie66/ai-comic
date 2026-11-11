#!/usr/bin/env bash
# Restore only into a new/empty directory and, for PG, a new empty database.
set -euo pipefail
umask 077
if [[ "${1:-}" != --confirm-empty-targets || $# -ne 3 ]]; then
  echo 'Usage: restore.sh --confirm-empty-targets BACKUP_DIRECTORY EMPTY_DATA_ROOT' >&2; exit 2
fi
backup_dir="$(cd -- "$2" && pwd)"
[[ "$(cat "$backup_dir/STATUS")" == complete ]] || { echo 'Backup incomplete' >&2; exit 2; }
target="$3"
mkdir -p -- "$target"
target="$(cd -- "$target" && pwd)"
if [[ -n "$(ls -A -- "$target")" ]]; then echo 'Data target must be empty' >&2; exit 2; fi
if [[ -f "$backup_dir/postgres.dump" ]]; then
  database_url="${GO_SERVICE_DATABASE_URL:-${DATABASE_URL:-}}"
  : "${database_url:?Set the NEW empty target database URL}"
  command -v psql >/dev/null
  command -v pg_restore >/dev/null
  table_count="$(PGDATABASE="$database_url" psql -X -v ON_ERROR_STOP=1 -Atc "SELECT count(*) FROM pg_tables WHERE schemaname NOT IN ('pg_catalog','information_schema')")"
  [[ "$table_count" == 0 ]] || { echo 'Database target must have no application tables' >&2; exit 2; }
  PGDATABASE="$database_url" pg_restore --dbname="$database_url" --exit-on-error --no-owner --no-acl "$backup_dir/postgres.dump"
fi
tar -xzf "$backup_dir/data.tar.gz" -C "$target"
printf 'Restored into %s; review ownership and config before starting.\n' "$target"
