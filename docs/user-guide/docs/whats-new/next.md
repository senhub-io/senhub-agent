# Next (unreleased)

Nothing released yet since 0.6.2. Changes land here as they are merged to `dev`.

<div class="rn-filter"></div>
- **host.id is always lowercase.** The `/etc/machine-id` fallback was not lowercased; the same id in two cases would be two hosts downstream (no host in production was affected).
