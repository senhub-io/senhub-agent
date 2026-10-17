#!/usr/bin/env bash
# Runs the mysql probe integration suite against throwaway servers: for each
# image it starts a container, creates the monitoring user with the grants
# the probe page documents, runs the tests, removes the container.
#
#   MYSQL_IMAGES   space-separated images to run against
#                  (default "mysql:8.4 mariadb:11"; the suite covers both
#                  engines because MariaDB has no @@server_uuid)
#   MYSQL_KEEP=1   leave the last container running afterwards
#   MYSQL_REPLICA=0  skip the GTID replica scenario (default 1: for each
#                  mysql image a second container replicates from the first
#                  on a dedicated docker network; MariaDB is not covered
#                  because its GTID setup differs)
#
# The host port is chosen by docker, so concurrent runs do not collide.
set -euo pipefail

images="${MYSQL_IMAGES:-mysql:8.4 mariadb:11}"
root_password="Root_$$_pw"
mon_user="senhub_monitor"
mon_password="Mon1tor_$$_pw"

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../../../.." && pwd)"

command -v docker >/dev/null || { echo "docker is required" >&2; exit 2; }

with_replica="${MYSQL_REPLICA:-1}"
name=""
replica_name=""
network=""
repl_user="senhub_repl"
repl_password="Repl_$$_pw"
cleanup() {
  [ -n "$name" ] || return 0
  if [ "${MYSQL_KEEP:-0}" = 1 ]; then
    echo "containers $name $replica_name left running"
  else
    [ -z "$replica_name" ] || docker rm -f "$replica_name" >/dev/null 2>&1 || true
    docker rm -f "$name" >/dev/null 2>&1 || true
    [ -z "$network" ] || docker network rm "$network" >/dev/null 2>&1 || true
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
  replica_name=""
  network=""
  replicate=0
  if [ "$with_replica" = 1 ] && [ "$client" = mysql ]; then
    replicate=1
    replica_name="$name-replica"
    network="$name-net"
    docker network create "$network" >/dev/null
  fi

  echo "starting $image as $name"
  net_args=()
  gtid_args=()
  if [ "$replicate" = 1 ]; then
    net_args=(--network "$network")
    gtid_args=(--server-id=1 --gtid-mode=ON --enforce-gtid-consistency=ON)
  fi
  docker run -d --rm --name "$name" ${net_args[@]+"${net_args[@]}"} -p 127.0.0.1::3306 \
    -e MYSQL_ROOT_PASSWORD="$root_password" -e MARIADB_ROOT_PASSWORD="$root_password" \
    "$image" ${gtid_args[@]+"${gtid_args[@]}"} >/dev/null

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

  # The standalone scenarios assume a source nobody replicates from, so
  # the replica joins only afterwards.
  if [ "$replicate" = 1 ]; then
    echo "starting $replica_name as a GTID replica of $name"
    # SHOW REPLICAS, which the probe uses to count downstream replicas,
    # requires REPLICATION SLAVE on top of the documented grants.
    docker exec -i "$name" mysql -h127.0.0.1 -uroot -p"$root_password" \
      -e "GRANT REPLICATION SLAVE ON *.* TO '$mon_user'@'%'"
    docker exec -i "$name" mysql -h127.0.0.1 -uroot -p"$root_password" <<SQL
CREATE USER '$repl_user'@'%' IDENTIFIED BY '$repl_password';
GRANT REPLICATION SLAVE ON *.* TO '$repl_user'@'%';
SQL
    docker run -d --rm --name "$replica_name" --network "$network" -p 127.0.0.1::3306 \
      -e MYSQL_ROOT_PASSWORD="$root_password" "$image" \
      --server-id=2 --gtid-mode=ON --enforce-gtid-consistency=ON >/dev/null

    ready=0
    for _ in $(seq 1 90); do
      if docker exec "$replica_name" mysql -h127.0.0.1 -uroot -p"$root_password" -e 'SELECT 1' >/dev/null 2>&1; then
        ready=1
        break
      fi
      if [ "$(docker inspect -f '{{.State.Running}}' "$replica_name" 2>/dev/null)" != true ]; then
        docker logs "$replica_name" 2>&1 | tail -20 >&2
        echo "the replica container stopped" >&2
        exit 1
      fi
      sleep 2
    done
    [ "$ready" = 1 ] || { echo "the replica did not come up in 3 minutes" >&2; exit 1; }

    # The monitoring account reaches the replica through replication, as
    # every other statement does.
    docker exec -i "$replica_name" mysql -h127.0.0.1 -uroot -p"$root_password" <<SQL
CHANGE REPLICATION SOURCE TO SOURCE_HOST='$name', SOURCE_USER='$repl_user',
  SOURCE_PASSWORD='$repl_password', SOURCE_AUTO_POSITION=1, GET_SOURCE_PUBLIC_KEY=1;
START REPLICA;
SQL

    replica_hostport="$(docker port "$replica_name" 3306/tcp | head -1)"
    export MYSQL_REPLICA_TEST_PORT="${replica_hostport##*:}"
    echo "running the replication scenarios against $image source and replica"
    (cd "$repo_root" && go test -tags integration -count=1 -v -run 'TestIntegration_Replication_' ./internal/agent/probes/mysql/) || status=1
    unset MYSQL_REPLICA_TEST_PORT
  fi

  cleanup
  name=""
done
exit "$status"
