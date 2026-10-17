#!/usr/bin/env bash
# Runs the postgresql probe integration suite against a throwaway server:
# it starts a container, creates the monitoring role with pg_monitor (the
# grant the probe page documents), runs the tests, removes the container.
#
#   POSTGRESQL_IMAGE   image to start (default postgres:17)
#   POSTGRESQL_KEEP=1  leave the containers running afterwards
#   POSTGRESQL_REPLICA=0  skip the streaming-replica scenario (default 1: it
#                      adds a second container streaming from the first on a
#                      dedicated docker network, about 15 seconds)
#
# The host port is chosen by docker, so concurrent runs do not collide.
set -euo pipefail

image="${POSTGRESQL_IMAGE:-postgres:17}"
admin_password="Admin_$$_pw"
mon_user="senhub_monitor"
mon_password="Mon1tor_$$_pw"

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../../../.." && pwd)"

command -v docker >/dev/null || { echo "docker is required" >&2; exit 2; }

with_replica="${POSTGRESQL_REPLICA:-1}"
name="senhub-postgresql-it-$$"
replica_name="$name-replica"
network="$name-net"
repl_user="senhub_repl"
repl_password="Repl_$$_pw"
cleanup() {
  if [ "${POSTGRESQL_KEEP:-0}" = 1 ]; then
    echo "containers $name $replica_name left running on network $network"
  else
    docker rm -f "$replica_name" "$name" >/dev/null 2>&1 || true
    docker network rm "$network" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

echo "starting $image as $name"
network_args=()
if [ "$with_replica" = 1 ]; then
  docker network create "$network" >/dev/null
  network_args=(--network "$network")
fi
docker run -d --rm --name "$name" ${network_args[@]+"${network_args[@]}"} -p 127.0.0.1::5432 \
  -e POSTGRES_PASSWORD="$admin_password" "$image" \
  -c wal_level=replica -c max_wal_senders=5 -c hot_standby=on >/dev/null

echo "waiting for the server"
ready=0
for _ in $(seq 1 90); do
  # The official image restarts the server once after its init scripts;
  # a TCP query as the admin user only succeeds on the final instance.
  if docker exec "$name" psql -h 127.0.0.1 -U postgres -tAc 'SELECT 1' >/dev/null 2>&1; then
    ready=1
    break
  fi
  if [ "$(docker inspect -f '{{.State.Running}}' "$name" 2>/dev/null)" != true ]; then
    docker logs "$name" 2>&1 | tail -20 >&2
    echo "the container stopped" >&2
    exit 1
  fi
  sleep 2
done
[ "$ready" = 1 ] || { echo "$image did not come up in 3 minutes" >&2; exit 1; }

echo "creating $mon_user with pg_monitor"
docker exec -i "$name" psql -h 127.0.0.1 -U postgres -v ON_ERROR_STOP=1 <<SQL
CREATE ROLE $mon_user LOGIN PASSWORD '$mon_password';
GRANT pg_monitor TO $mon_user;
SQL

hostport="$(docker port "$name" 5432/tcp | head -1)"
export POSTGRESQL_TEST_HOST=127.0.0.1
export POSTGRESQL_TEST_PORT="${hostport##*:}"
export POSTGRESQL_TEST_USER="$mon_user"
export POSTGRESQL_TEST_PASSWORD="$mon_password"

echo "running the suite against $image on port $POSTGRESQL_TEST_PORT"
cd "$repo_root"
status=0
go test -tags integration -count=1 -v ./internal/agent/probes/postgresql/ || status=1

# The standalone scenarios above assume a primary nobody streams from, so
# the replica joins only afterwards.
if [ "$with_replica" = 1 ]; then
  echo "creating $repl_user and starting $replica_name as a streaming replica"
  docker exec -i "$name" psql -h 127.0.0.1 -U postgres -v ON_ERROR_STOP=1 <<SQL
CREATE ROLE $repl_user REPLICATION LOGIN PASSWORD '$repl_password';
SQL
  # The image's pg_hba.conf only admits replication connections from
  # localhost.
  docker exec "$name" bash -c \
    'echo "host replication all all scram-sha-256" >> "$PGDATA/pg_hba.conf"'
  docker exec "$name" psql -h 127.0.0.1 -U postgres -tAc 'SELECT pg_reload_conf()' >/dev/null

  # pg_basebackup -R writes standby.signal and primary_conninfo; the stock
  # entrypoint then starts the cloned data directory as a standby.
  docker run -d --rm --name "$replica_name" --network "$network" -p 127.0.0.1::5432 \
    -e PGPASSWORD="$repl_password" -e PRIMARY_HOST="$name" -e REPL_USER="$repl_user" \
    --entrypoint bash "$image" -c \
    'gosu postgres pg_basebackup -h "$PRIMARY_HOST" -U "$REPL_USER" -D "$PGDATA" -X stream -R \
       && exec docker-entrypoint.sh postgres' >/dev/null

  ready=0
  for _ in $(seq 1 60); do
    if [ "$(docker exec "$replica_name" psql -h 127.0.0.1 -U "$mon_user" -d postgres -tAc 'SELECT pg_is_in_recovery()' 2>/dev/null || true)" = t ]; then
      ready=1
      break
    fi
    if [ "$(docker inspect -f '{{.State.Running}}' "$replica_name" 2>/dev/null)" != true ]; then
      docker logs "$replica_name" 2>&1 | tail -20 >&2
      echo "the replica container stopped" >&2
      exit 1
    fi
    sleep 1
  done
  [ "$ready" = 1 ] || { echo "the replica did not reach recovery in 1 minute" >&2; exit 1; }

  # A commit replayed on the standby is what gives it a replay timestamp.
  docker exec "$name" psql -h 127.0.0.1 -U postgres -v ON_ERROR_STOP=1 \
    -c 'CREATE TABLE replica_probe_marker (id int)' >/dev/null

  replica_hostport="$(docker port "$replica_name" 5432/tcp | head -1)"
  export POSTGRESQL_REPLICA_TEST_PORT="${replica_hostport##*:}"
fi

if [ "$with_replica" = 1 ]; then
  echo "running the replication scenarios against the primary and its replica"
  go test -tags integration -count=1 -v -run 'TestIntegration_Replication_' ./internal/agent/probes/postgresql/ || status=1
fi
exit "$status"
