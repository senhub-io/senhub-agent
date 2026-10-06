# cloud-init

cloud-init reads a `user-data` document the first time a virtual machine
boots, which makes it the natural way to install the agent on machines
created from a cloud image: Ubuntu, Debian, RHEL, Rocky Linux and
AlmaLinux. It runs once per instance, so everything here is a first-boot
setup. A later change belongs to another tool (see
[Upgrade](#upgrade)).

The example installs the agent from the signed package repository, pins the
repository key by its fingerprint, writes a probe and an output, checks the
configuration, and only then applies it.

## The user-data

Save as `user-data.yaml`. The three values at the top of the script are the
ones to change.

```yaml
#cloud-config
packages: [curl, gnupg]
write_files:
  - path: /var/lib/cloud/senhub/50-cloud-init.probes.yaml
    permissions: "0600"
    content: |
      - name: orders-db
        type: postgresql
        params:
          host: db.example.com
          username: monitor
          password: "${file:/etc/senhub-agent/secrets/pg_password}"
          interval: 60
  - path: /var/lib/cloud/senhub/50-cloud-init.otlp.yaml
    permissions: "0600"
    content: |
      otlp:
        endpoint: collector.example.com:4317
  - path: /var/lib/cloud/senhub/pg_password
    permissions: "0600"
    content: "<database password>"
  - path: /var/lib/cloud/senhub/license.jwt
    permissions: "0600"
    content: "<licence token, or delete this entry for the free tier>"
  - path: /usr/local/sbin/senhub-bootstrap.sh
    permissions: "0700"
    content: |
      #!/bin/sh
      set -eu

      SENHUB_VERSION="${SENHUB_VERSION:-0.6.2~beta.1}"  # exact package version: the pin
      SENHUB_EDITION="${SENHUB_EDITION:-senhub-agent-oss}"  # or senhub-agent
      SENHUB_CHANNEL="${SENHUB_CHANNEL:-beta}"       # stable once 0.6.2 is published
      FINGERPRINT="B9987A2D4623796E19D3B185CA56F750354530AF"
      SRC=/var/lib/cloud/senhub
      CFG=/etc/senhub-agent
      trap 'rm -rf "$SRC"' EXIT      # the staged secrets never outlive the script

      # 1. The repository key is trusted only if it carries the published
      #    fingerprint (compare it with https://packages.senhub.io).
      key=$(mktemp)
      curl -fsSL https://packages.senhub.io/gpg.key -o "$key"
      if ! gpg --show-keys --with-colons --with-fingerprint "$key" \
           | grep -q "^fpr:::::::::${FINGERPRINT}:"; then
        echo "repository key refused: unexpected fingerprint" >&2
        exit 1
      fi

      # 2. Repository and package, at the pinned version.
      if command -v apt-get >/dev/null 2>&1; then
        install -d -m 0755 /etc/apt/keyrings
        gpg --dearmor --yes -o /etc/apt/keyrings/senhub.gpg < "$key"
        echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/senhub.gpg] https://packages.senhub.io/apt ${SENHUB_CHANNEL} main" \
          > /etc/apt/sources.list.d/senhub.list
        apt-get update -qq
        DEBIAN_FRONTEND=noninteractive apt-get install -y "${SENHUB_EDITION}=${SENHUB_VERSION}*"
        apt-mark hold "${SENHUB_EDITION}"
      else
        rpm --import "$key"
        curl -fsSLo /etc/yum.repos.d/senhub.repo \
          "https://packages.senhub.io/rpm/${SENHUB_CHANNEL}/senhub.repo"
        dnf install -y "${SENHUB_EDITION}-${SENHUB_VERSION}"
      fi
      rm -f "$key"

      # 3. The secret must exist where the probe points before the check.
      install -d -o senhub -g senhub -m 0750 "$CFG/secrets"
      install -o senhub -g senhub -m 0600 "$SRC/pg_password" "$CFG/secrets/pg_password"

      # 4. Build the configuration in a scratch copy and check it there.
      stage=$(mktemp -d)
      cp -a "$CFG/." "$stage/"
      cp "$SRC/50-cloud-init.probes.yaml" "$stage/probes.d/"
      cp "$SRC/50-cloud-init.otlp.yaml" "$stage/strategies.d/"
      [ -f "$SRC/license.jwt" ] && cp "$SRC/license.jwt" "$stage/license.jwt"
      rc=0
      senhub-agent config check --json "$stage/agent.yaml" || rc=$?
      if [ "$rc" -eq 2 ]; then
        echo "configuration refused by config check, nothing applied" >&2
        rm -rf "$stage"
        exit 1
      fi

      # 5. Apply: files first, then the service picks them up.
      install -o senhub -g senhub -m 0640 "$SRC/50-cloud-init.probes.yaml" "$CFG/probes.d/"
      install -o senhub -g senhub -m 0640 "$SRC/50-cloud-init.otlp.yaml" "$CFG/strategies.d/"
      if [ -f "$SRC/license.jwt" ]; then
        install -o senhub -g senhub -m 0640 "$SRC/license.jwt" "$CFG/license.jwt"
      fi
      rm -rf "$stage"
      systemctl enable senhub-agent
      systemctl restart senhub-agent
runcmd:
  - [ /usr/local/sbin/senhub-bootstrap.sh ]
```

What it does, in the order the script runs:

1. `write_files` stages the fragments, the secret and the licence in
   `/var/lib/cloud/senhub/`, a directory the package does not own, so the
   package's directory ownership is never in conflict.
2. The key of the repository is downloaded and its fingerprint compared with
   the published one before anything trusts it; a mismatch stops the script.
3. The package is installed at the pinned version.
4. The configuration is assembled in a scratch copy of `/etc/senhub-agent`
   and checked there.
5. Only on exit `0` or `1` are the files copied to the live directory and the
   service restarted. The agent also loads fragment changes by itself; the
   restart is for the licence, which is read at start.

The packages start the service at install with the default configuration
(host probes, local HTTP). Between the package install and the apply, the
agent runs that default, which pushes nowhere: nothing the check refuses
ever runs.

## Pinning

`SENHUB_VERSION` is the pin, in the package's spelling. The script installs `0.6.2~beta.1` exactly and holds
the package (`apt-mark hold`), so `apt upgrade` and unattended upgrades do
not move it. On RHEL, add the versionlock plugin to get the same effect:

```bash
sudo dnf install -y python3-dnf-plugin-versionlock
sudo dnf versionlock add senhub-agent-oss
```

To see which versions the repository offers:

```bash
apt-cache madison senhub-agent-oss     # Debian, Ubuntu
sudo dnf --showduplicates list senhub-agent-oss   # RHEL family
```

A beta is spelled `0.6.2~beta.1` in the packages and `0.6.2-beta.1` as a
release tag. The example defaults to the beta channel because the stable
channel is not published yet (its repository answers 404 until the
0.6.2 release); it comes with the 0.6.2 release, and then
`SENHUB_CHANNEL=stable` and `SENHUB_VERSION=0.6.2` apply.

## Licence

The free tier needs none: delete the `license.jwt` entry of `write_files`.
For a licence, the token is a secret and **user-data is not a safe place for
it** on most clouds: it is shown in the console of the provider and readable
from the instance metadata service by any process on the machine. Two
options:

- Keep the entry above and accept that exposure, which suits a private
  network and a licence you can rotate.
- Remove the entry and fetch the token from your secret store in the script,
  before step 4, for example a pre-signed URL or the provider's secrets
  CLI, writing to `$SRC/license.jwt`:

  ```sh
  curl -fsS "$LICENSE_URL" -o "$SRC/license.jwt"
  ```

The script deletes `$SRC` once the licence is installed, so no copy stays in
the staging directory. cloud-init keeps its own copy of the user-data under
`/var/lib/cloud/instances/`, readable by root only.

## Secrets

The probe refers to the password with `${file:...}`; the agent reads the
file at every start and trims the trailing newline. The value never appears
in a fragment, in `config show` (which masks it) or in the process list.

The same rule works without any file, from the environment of the unit, for
a host that already receives variables from the provider:

```ini
# systemd drop-in written by write_files
[Service]
Environment=SENHUB_PROBE_ORDERSDB_TYPE=postgresql
Environment=SENHUB_PROBE_ORDERSDB_HOST=db.example.com
Environment=SENHUB_PROBE_ORDERSDB_USERNAME=monitor
Environment=SENHUB_PROBE_ORDERSDB_PASSWORD_FILE=/etc/senhub-agent/secrets/pg_password
```

Place it in `/etc/systemd/system/senhub-agent.service.d/10-probes.conf`,
then `systemctl daemon-reload` and restart the service. See
[Probes from environment variables](../configuration.md#configuring-probes-from-environment-variables).

## Validation

Two checks, one before the machine exists and one on it.

Before: validate the user-data against the cloud-init schema, on any machine
that has cloud-init 22.2 or later.

```bash
cloud-init schema --config-file user-data.yaml --annotate
```

On the machine: the script's step 4 is the gate, and you can run it again by
hand on a running host, which changes nothing:

```bash
sudo senhub-agent config check --json
echo "exit $?"
```

Exit `0` is clean, `1` is warnings only (applied), `2` is an error (refused,
and the script stops with the reason on standard error, visible in
`/var/log/cloud-init-output.log`).

## Upgrade

cloud-init does not run again on an existing instance. Move the version with
the package manager, on purpose:

```bash
sudo apt-mark unhold senhub-agent-oss
sudo apt-get install -y --allow-downgrades "senhub-agent-oss=0.6.2*"
sudo apt-mark hold senhub-agent-oss
```

or `sudo dnf install -y senhub-agent-oss-0.6.2` (for a beta, the tilde form, `senhub-agent-oss-0.6.2~beta.2`) after
`dnf versionlock delete senhub-agent-oss`. The package keeps
`/etc/senhub-agent`, the agent key and the licence, and restarts the
service. For a fleet, drive this with Ansible (the
[role](../ansible.md) does exactly this) or replace the instances from a
template with a new `SENHUB_VERSION`.

## Removal

```bash
sudo apt-mark unhold senhub-agent-oss
sudo apt-get remove -y senhub-agent-oss      # keeps /etc/senhub-agent and the state
sudo apt-get purge -y senhub-agent-oss       # also deletes them, and the senhub user
```

On RHEL: `sudo dnf remove -y senhub-agent-oss`. Remove
`/etc/apt/sources.list.d/senhub.list` and `/etc/apt/keyrings/senhub.gpg`
(or `/etc/yum.repos.d/senhub.repo`) to drop the repository.

## Verify

Run on the new machine, after the first boot has finished
(`cloud-init status --wait`).

```bash
cloud-init status --long
systemctl is-active senhub-agent
senhub-agent --version
dpkg -l senhub-agent-oss | tail -1        # or: rpm -q senhub-agent-oss
apt-mark showhold                          # lists senhub-agent-oss
sudo senhub-agent config check; echo "exit $?"
sudo senhub-agent license show
sudo ls -l /etc/senhub-agent/probes.d/ /etc/senhub-agent/strategies.d/
curl -fsS http://127.0.0.1:8080/health
test ! -e /var/lib/cloud/senhub && echo "staging directory removed"
```

On the RHEL family, replace the two package lines by the first one below,
and add the second only if you installed the versionlock plugin (see
Pinning); without it, dnf answers "No such command: versionlock":

```bash
rpm -q senhub-agent-oss
sudo dnf versionlock list    # only with python3-dnf-plugin-versionlock
```

Expected: `status: done`, the service `active`, the pinned version, the hold
listed, `config check` exit `0`, the two `50-cloud-init.*` fragments present,
`/health` answering and no staging directory left.
