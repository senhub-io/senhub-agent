# Next (unreleased)

Changes land here as they are merged to `dev`.

<div class="rn-filter"></div>

## Changes

- **Listeners are linked to the address they bind.** A listener bound to a
  routable IP address now carries a `bound_to` relation to that
  `network.address`, in addition to the interface it listens on. Wildcard,
  loopback, link-local and container-bridge binds are unchanged.
