#!/usr/bin/env bash
# Runs the oracle probe integration suite against a throwaway Oracle
# container: start it, create the monitoring user with the grants the probe
# page documents, run the tests, stop the container.
#
#   ORACLE_IMAGE     image to start (default gvenzl/oracle-free:23-slim; also
#                    gvenzl/oracle-xe:21-slim, which is amd64 only)
#   ORACLE_SERVICE   service name (default: FREEPDB1 for an oracle-free image,
#                    XEPDB1 for an oracle-xe one)
#   ORACLE_PORT      host port to publish (default 15210)
#   ORACLE_MONITOR_PASSWORD  password of the monitoring user (default: over 30 characters)
#   ORACLE_KEEP=1    leave the container running afterwards
#
# The monitoring user gets a password over 30 characters on purpose: it is the
# case that Oracle 23ai accepts and a driver that does not announce long
# password support gets refused with ORA-01017.
set -euo pipefail

image="${ORACLE_IMAGE:-gvenzl/oracle-free:23-slim}"
port="${ORACLE_PORT:-15210}"
case "$image" in
  *oracle-xe*) default_service=XEPDB1 ;;
  *) default_service=FREEPDB1 ;;
esac
service="${ORACLE_SERVICE:-$default_service}"

name="senhub-oracle-it-$$"
sys_password="SysPassw0rd_$$"
mon_user="senhub"
mon_password="${ORACLE_MONITOR_PASSWORD:-Mon1tor_0123456789_abcdefghijklmnopqrstuvwxyz}"

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../../../.." && pwd)"

command -v docker >/dev/null || { echo "docker is required" >&2; exit 2; }

cleanup() {
  if [ "${ORACLE_KEEP:-0}" = 1 ]; then
    echo "container $name left running on port $port"
  else
    docker rm -f "$name" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

echo "starting $image as $name"
docker run -d --name "$name" -p "$port:1521" -e ORACLE_PASSWORD="$sys_password" "$image" >/dev/null

echo "waiting for the database (first start takes a few minutes)"
for _ in $(seq 1 180); do
  if docker logs "$name" 2>&1 | grep -q "DATABASE IS READY TO USE"; then
    ready=1
    break
  fi
  if [ "$(docker inspect -f '{{.State.Running}}' "$name")" != true ]; then
    docker logs "$name" 2>&1 | tail -20 >&2
    echo "the container stopped" >&2
    exit 1
  fi
  sleep 5
done
[ "${ready:-0}" = 1 ] || { echo "the database did not come up in 15 minutes" >&2; exit 1; }

echo "creating $mon_user with the documented grants"
docker exec -i "$name" sqlplus -s "sys/$sys_password@//localhost/$service" as sysdba <<SQL
WHENEVER SQLERROR EXIT FAILURE
SET DEFINE OFF
CREATE USER $mon_user IDENTIFIED BY "$mon_password";
GRANT CREATE SESSION TO $mon_user;
GRANT SELECT ON V_\$INSTANCE TO $mon_user;
GRANT SELECT ON V_\$SESSION TO $mon_user;
GRANT SELECT ON V_\$RESOURCE_LIMIT TO $mon_user;
GRANT SELECT ON V_\$PARAMETER TO $mon_user;
GRANT SELECT ON V_\$SYSSTAT TO $mon_user;
GRANT SELECT ON V_\$SGASTAT TO $mon_user;
GRANT SELECT ON V_\$PGASTAT TO $mon_user;
GRANT SELECT ON V_\$SYSTEM_WAIT_CLASS TO $mon_user;
GRANT SELECT ON DBA_TABLESPACES TO $mon_user;
GRANT SELECT ON DBA_TABLESPACE_USAGE_METRICS TO $mon_user;
EXIT
SQL

export ORACLE_TEST_HOST=127.0.0.1
export ORACLE_TEST_PORT="$port"
export ORACLE_TEST_SERVICE="$service"
export ORACLE_TEST_USER="$mon_user"
export ORACLE_TEST_PASSWORD="$mon_password"

cd "$repo_root"
go test -tags integration -count=1 -v ./internal/agent/probes/oracle/
