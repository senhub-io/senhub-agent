# Next (unreleased)

<div class="rn-filter"></div>


## Fixed

### `refresh-unit` no longer disarms a `--user root` install

`senhub-agent install --user root` writes a unit that runs with full
privileges — which is the entire reason to choose it, for probes that need raw
ICMP sockets or a privileged port. Running `senhub-agent refresh-unit` on such a
host rewrote it to the hardened template with `User=root`, and the hardened
template drops every Linux capability. The service kept starting, so nothing
looked wrong; the active checks that needed those capabilities simply stopped
working.

A refresh on a root install now produces the same unit the install produced,
capabilities included. Root identity is expressed the way the installer
expresses it — by the absence of a `User=` directive, systemd's default being
root — instead of an explicit `User=root` on a capability-dropping unit.

Non-root installs are unchanged: the `senhub` user still gets the hardened unit
verbatim, and a custom service user still gets it re-templated. A refresh still
never switches a root install to the `senhub` user. (#689)
