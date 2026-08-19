# Next (unreleased)

Changes land here as they are merged to `dev`.

<div class="rn-filter"></div>

## Features

- **Push OTLP straight to a backend that serves it under a base path.**
  `endpoint` is a `host:port` pair and cannot carry a path, which ruled
  out backends exposing OTLP under a prefix. The new
  `url_path_prefix` (OTLP/HTTP only) is prepended to the standard signal
  paths, so `/api/v2/otlp` sends to `/api/v2/otlp/v1/metrics` and its
  siblings. Setting it with `protocol: grpc` is refused at config load
  rather than silently ignored. Documented with a Dynatrace example.
