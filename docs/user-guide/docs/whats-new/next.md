# Next (unreleased)

<div class="rn-filter"></div>


## Fixed

### `filetail` can read the system log files again on a hardened install

On Debian and Ubuntu, `/var/log/syslog` and `/var/log/auth.log` belong to
`syslog:adm` with mode `0640`, so a `filetail` probe pointed at them collected
nothing under the non-root unit — silently, with no error naming the cause. The
journal was never affected: `linux_logs` reads it through the `systemd-journal`
group the unit already grants.

`senhub-agent install`, `senhub-agent refresh-unit` and the `.deb` / `.rpm`
postinstall now join the service user to the `adm` group, which grants exactly
those log files and nothing else. Running `refresh-unit` is how an existing
install picks it up. Where the group does not exist, the join is skipped and the
install still succeeds.

The membership is granted through the user database rather than the unit's
`SupplementaryGroups=`, because a `SupplementaryGroups=` naming a group absent
from the distribution fails the unit at startup with `216/GROUP`.

The admin guide now documents the grant, and warns against the workaround it
replaces: `CAP_DAC_READ_SEARCH` does make the logs readable, but it bypasses
every file read permission check on the host — `/etc/shadow`, private keys and
any customer data included. On Red Hat systems `rsyslog` writes
`/var/log/messages` as `root:root 0600`, where `adm` does not help and
`linux_logs` is the answer. (#732)
