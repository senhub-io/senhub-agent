# Next (unreleased)

Nothing released yet since 0.6.0. Changes land here as they are merged to `dev`.

<div class="rn-filter"></div>

## Fixes

- **The container image answers PRTG, Nagios and Prometheus from outside.**
  Its HTTP output listened on the container's loopback, which nothing
  outside the container reaches: a published port answered "connection
  refused". The image now listens on every address, set with
  `SENHUB_HTTP_BIND`; `config init` takes `--http-bind`.
