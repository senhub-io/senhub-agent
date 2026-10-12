# Next (unreleased)

Changes land here as they are merged to `dev`.

<div class="rn-filter"></div>

## Changes

- **Listeners are linked to the address they bind.** A listener bound to a
  routable IP address now carries a `bound_to` relation to that
  `network.address`, in addition to the interface it listens on. Wildcard,
  loopback, link-local and container-bridge binds are unchanged.
- **Identity re-keys are announced once, not at every start.** The retirement of the legacy database and service identities (0.5.4 re-key) was repeated during the first cycles of every agent start, which a topology backend read as the producer reporting the node gone. A marker file in the state directory now records that it was announced; if the directory is not writable the announcement repeats and one warning is logged.
