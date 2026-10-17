#!/usr/bin/env bash
# Runs the mysql probe integration suite against throwaway servers: for each
# image it starts a container, creates the monitoring user with the grants
# the probe page documents, runs the tests, removes the container.
#
#   MYSQL_IMAGES   space-separated images to run against
#                  (default "mysql:8.4 mariadb:11"; the suite covers both
#                  engines because MariaDB has no @@server_uuid)
#   MYSQL_KEEP=1   leave the last container running afterwards
#
# The host port is chosen by docker, so concurrent runs do not collide.
set -euo pipefail

images="${MYSQL_IMAGES:-mysql:8.4 mariadb:11}"
root_password="Root_$$_pw"
mon_user="senhub_monitor"
mon_password="Mon1tor_$$_pw"

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../../../.." && pwd)"

command -v docker >/dev/null || { echo "docker is required" >&2; exit 2; }

name=""
cleanup() {
  [ -n "$name" ] || return 0
  if [ "${MYSQL_KEEP:-0}" = 1 ]; then
    echo "container $name left running"
  else
    docker rm -f "$name" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

status=0
for image in $images; do
  case "$image" in
    mariadb*) client=mariadb ;;
    *) client=mysql ;;
  esac
  name="senhub-mysql-it-$$"

  echo "starting $image as $name"
  docker run -d --rm --name "$name" -p 127.0.0.1::3306 \
    -e MYSQL_ROOT_PASSWORD="$root_password" -e MARIADB_ROOT_PASSWORD="$root_password" \
    "$image" >/dev/null

  echo "waiting for the server"
  ready=0
  for _ in $(seq 1 90); do
    if docker exec "$name" "$client" -h127.0.0.1 -uroot -p"$root_password" -e 'SELECT 1' >/dev/null 2>&1; then
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

  echo "creating $mon_user with the documented grants"
  docker exec -i "$name" "$client" -h127.0.0.1 -uroot -p"$root_password" <<SQL
CREATE USER '$mon_user'@'%' IDENTIFIED BY '$mon_password';
GRANT PROCESS, REPLICATION CLIENT, SELECT ON *.* TO '$mon_user'@'%';
SQL

  hostport="$(docker port "$name" 3306/tcp | head -1)"
  export MYSQL_TEST_HOST=127.0.0.1
  export MYSQL_TEST_PORT="${hostport##*:}"
  export MYSQL_TEST_USER="$mon_user"
  export MYSQL_TEST_PASSWORD="$mon_password"

  echo "running the suite against $image on port $MYSQL_TEST_PORT"
  (cd "$repo_root" && go test -tags integration -count=1 -v ./internal/agent/probes/mysql/) || status=1

  cleanup
  name=""
done
exit "$status"
