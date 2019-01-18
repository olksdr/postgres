#!/usr/bin/env bash

set -e
echo "-------------- Hostname: $HOSTNAME"

export PGPASSWORD=${POSTGRES_PASSWORD:-postgres}

EXIT_CODE=0
IsInRecovery=$(psql -U postgres -qtAX -c "SELECT pg_is_in_recovery();") || EXIT_CODE=$?

if [[ ${IsInRecovery} == 'f' ]]; then
  # current node is in master mode
  exit 0
elif [[ ${IsInRecovery} == 't' ]] || ([[ $EXIT_CODE != 0 ]] && [[ ${STANDBY:-} == "warm" ]]); then
  # current node is in standby mode
  state=$(psql -h "$PRIMARY_HOST" -U postgres -qtAX -c "select state from pg_stat_replication where pg_stat_replication.application_name='$HOSTNAME';")
  if [[ ${state} == 'streaming' ]]; then
    exit 0
  fi
fi

exit 1
