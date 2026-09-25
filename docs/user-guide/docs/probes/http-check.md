<img src="../../assets/probe-logos/http-check.svg" alt="" class="probe-page-logo probe-page-logo-mdi">

# http_check — HTTP(S) checks with TLS expiry

Free tier. Checks a list of URLs every cycle: status validation, latency
broken down by phase (DNS, connect, TLS handshake, time-to-first-byte,
total), response size, optional content matching — and the remaining
validity of the TLS certificate as a first-class metric.

## Quick start

```yaml
# probes.d/30-web.yaml
- name: web-checks
  type: http_check
  params:
    targets:
      - "https://www.example.com"
      - "https://api.example.com/healthz"
    interval: 60
```

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `targets` | Yes | - | URLs to check. Example: `https://app.example.com/health` |
| `method` | No | `GET` | HTTP method. One of `GET`, `HEAD`, `POST`, `PUT`, `DELETE`, `OPTIONS`, `PATCH` |
| `timeout` | No | `10` | Whole-request budget in seconds |
| `interval` | No | `60` | Seconds between cycles |
| `expected_status` | No | - | Exact status that counts as up; empty means any 2xx or 3xx |
| `content_match` | No | - | Regular expression the body must match. Example: `"status":"ok"` |
| `insecure_skip_verify` | No | `false` | Accept self-signed certificates |

<!-- schema:params:end -->

`insecure_skip_verify` is for a lab, not for production. Targets are checked in parallel (bounded). Redirects are reported, not
followed: a 301 is the measured answer of the target.

## Metrics

One series per metric per target (`target` tag).

| Metric | Unit | Description |
|---|---|---|
| `senhub.httpcheck.up` | bool | Expected status (and content, if configured) |
| `senhub.httpcheck.status.code` | code | Last response status |
| `httpcheck.duration` | ms | Total request time |
| `senhub.httpcheck.duration.dns` / `.connect` / `.tls` / `.ttfb` | ms | Phase breakdown |
| `senhub.httpcheck.response.size` | B | Body size (1 MiB read cap) |
| `senhub.httpcheck.tls.expiry` | days | Days until the certificate expires — negative once expired. Alert under 30 |
| `senhub.httpcheck.content.match` | bool | Only when `content_match` is configured |

A failing or unreachable target is a measurement (`up = 0`), never a
probe failure.

## Metric reference

Every metric this probe can emit. The first column is the name the
OTLP and Prometheus outputs use, the second the channel the PRTG and
Nagios outputs carry.

<!-- schema:metrics:start -->
<!-- Generated from the probe's definition. Run `make docs-metrics` after changing it. -->

| Metric | Channel | Unit | Description |
|---|---|---|---|
| `senhub.httpcheck.up` | `httpcheck_up` | # | 1 when the target answered with the expected status (and content, if configured) |
| `senhub.httpcheck.status.code` | `httpcheck_status_code` | # | HTTP status code of the last response |
| `httpcheck.duration` | `httpcheck_duration` | ms | Wall-clock time of the whole request |
| `senhub.httpcheck.duration.dns` | `httpcheck_dns_time` | ms | DNS resolution phase |
| `senhub.httpcheck.duration.connect` | `httpcheck_connect_time` | ms | TCP connect phase |
| `senhub.httpcheck.duration.tls` | `httpcheck_tls_time` | ms | TLS handshake phase (HTTPS targets only) |
| `senhub.httpcheck.duration.ttfb` | `httpcheck_ttfb` | ms | Time from request start to the first response byte |
| `senhub.httpcheck.response.size` | `httpcheck_response_size` | B | Response body size (capped at 1 MiB read) |
| `senhub.httpcheck.tls.expiry` | `httpcheck_tls_expiry` | # | Days until the leaf certificate expires (negative once expired) |
| `senhub.httpcheck.tls.valid` | `httpcheck_tls_valid` | # | 1 when the leaf TLS certificate is currently valid (not expired), 0 otherwise |
| `senhub.httpcheck.content.match` | `httpcheck_content_match` | # | 1 when the configured content_match regexp matched the response body |

<!-- schema:metrics:end -->
