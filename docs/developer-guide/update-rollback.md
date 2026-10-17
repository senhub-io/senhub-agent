# Update rollback

Scope: the in-place binary replacement done by `doUpdate` (Windows ZIP
installs, and `sudo senhub-agent update` on Linux archive installs). The MSI
path hands off to msiexec and is not covered.

## Files

Next to the executable:

- `<binary>.previous`: copy of the binary that was running before the swap.
  Kept after a successful update, replaced by the next update.

In the state directory (`instanceid.OwnStateDir()`):

- `update-pending.json`: `{from_version, to_version, swapped_at, boot_attempts}`.
  Exists from the swap until the new version is confirmed.
- `update-rolled-back.json`: `{failed_version, restored_version, rolled_back_at, boot_attempts, outcome}`.
  Written when a rollback happened (`outcome: "restored"`) or was needed but
  could not be performed (`"not_performed"`). Shown by `senhub-agent doctor`.

All writes are atomic (temp file + rename), so a daemon running as another
user can replace a marker written by the root-run CLI.

## State machine (at agent start)

1. No pending marker: nothing happens.
2. Marker unreadable or corrupt: warn, delete it, nothing else.
3. Marker whose `to_version` is not the running version (the previous binary
   is running, or a later update ran): stale, delete it.
4. Marker for the running version: `boot_attempts++`, persisted.
   - If `boot_attempts >= 3` and `now - swapped_at <= 10 min`: rollback.
   - If `boot_attempts >= 3` but older than 10 min: not a crash loop; delete
     the marker, warn.
5. Once the supervisor has started every service and the process has stayed up
   120 s, the update is confirmed: the marker is deleted and any previous
   rolled-back record is removed. `.previous` stays for one more update.

The readiness signal is the supervisor start itself: it returns errors if a
service (probes, data store with its outputs) fails to start, and the agent
exits in that case. No health model is added.

## Rollback

On Windows, a running `.exe` cannot be overwritten but can be renamed:
`binary` is renamed to `.<binary>.failed`, `.previous` is renamed to `binary`;
if the second rename fails the first is undone. The pending marker is deleted,
the rolled-back record written, an Error logged with both versions, and the
agent exits non-zero so the service manager starts the restored binary.
`.<binary>.failed` is removed by the next start (leftovers).

On Linux the daemon never rewrites its executable (#794: the daemon is the
process exposed to untrusted input, and the binary is root-owned). The
detection still runs: the Error log and the record say the update never
confirmed, outcome `not_performed`, and the operator rolls back with
`sudo senhub-agent update <restored_version>`.

If `.previous` is missing, the record says `not_performed` and the agent keeps
running the new binary.

## No loop

The restored binary finds no pending marker. The rolled-back record makes
`Update()` skip any version that is not newer than `failed_version`, so the
hourly check does not roll forward again until a newer release appears. An
explicit `senhub-agent update <version>` from an operator is never skipped.
The protection lives in the code of the version that is restored to; a
restored binary older than this feature does not know the record.

## Service recovery

Windows: `app/service.go` configures recovery actions restart, restart,
restart, restart, none (reset period 24 h, delay 10 s). A crash loop gets
three failed boots and the rollback exit as the fourth failure, within the
four restarts. Linux units use `Restart=always`.
