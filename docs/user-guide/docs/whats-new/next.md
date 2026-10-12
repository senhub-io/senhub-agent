# Next (unreleased)

Changes land here as they are merged. The 0.6.3 corrective release collects them.

<div class="rn-filter"></div>

## Fixes

- **doctor resolves `${env:}` with the service's environment.** It reads `Environment=` and `EnvironmentFile=` from the systemd unit and its drop-ins (as root). A reference it cannot resolve outside the service is a warning that names the variable, never a failure.
