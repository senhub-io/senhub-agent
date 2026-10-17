#!/usr/bin/env bash
# Runs the postgresql probe integration suite against a throwaway server:
# it starts a container, creates the monitoring role with pg_monitor (the
# grant the probe page documents), runs the tests, removes the container.
#
#   POSTGRESQL_IMAGE   image to start (default postgres:17)
#   POSTGRESQL_KEEP=1  leave the container running afterwards
#
# The host port is chosen by docker, so concurrent runs do not collide.
set -euo pipefail

image="${POSTGRESQL_IMAGE:-postgres:17}"
admin_password="Admin_$$_pw"
mon_user="senhub_monitor"
mon_password="Mon1tor_$$_pw"

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../../../.." && pwd)"

command -v docker >/dev/null || { echo "docker is required" >&2; exit 2; }

name="senhub-postgresql-it-$$"
cleanup() {
  if [ "${POSTGRESQL_KEEP:-0}" = 1 ]; then
    echo "container $name left running"
  else
    docker rm -f "$name" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

echo "starting $image as $name"
docker run -d --rm --name "$name" -p 127.0.0.1::5432 \
  -e POSTGRES_PASSWORD="$admin_password" "$image" >/dev/null

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
go test -tags integration -count=1 -v ./internal/agent/probes/postgresql/
