#!/usr/bin/env bash

set -xe

echo "Running as Primary"

# set password ENV
export PGPASSWORD=${POSTGRES_PASSWORD:-postgres}

export ARCHIVE=${ARCHIVE:-}

if [ ! -e "$PGDATA/PG_VERSION" ]; then
  if [ "$RESTORE" = true ]; then
    echo "Restoring Postgres from base_backup using wal-g"
    /scripts/primary/restore.sh
  else
    /scripts/primary/start.sh
  fi
fi

# push base-backup
if [ "$ARCHIVE" == "wal-g" ]; then
  # set walg ENV
  CRED_PATH="/srv/wal-g/archive/secrets"
  export WALE_S3_PREFIX=$(echo "$ARCHIVE_S3_PREFIX")
  export AWS_ACCESS_KEY_ID=$(cat "$CRED_PATH/AWS_ACCESS_KEY_ID")
  export AWS_SECRET_ACCESS_KEY=$(cat "$CRED_PATH/AWS_SECRET_ACCESS_KEY")

  pg_ctl -D "$PGDATA" -w start
  PGUSER="postgres" wal-g backup-push "$PGDATA" >/dev/null
  pg_ctl -D "$PGDATA" -m fast -w stop
fi

# In already running primary server from previous releases, postgresql.conf may not contain 'wal_log_hints = on'
# Set it using 'sed'. ref: https://stackoverflow.com/a/11245501/4628962
sed -i '/wal_log_hints/c\wal_log_hints = on' $PGDATA/postgresql.conf

exec postgres
