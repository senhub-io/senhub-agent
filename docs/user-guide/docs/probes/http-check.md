<img src="https://api.iconify.design/mdi/web.svg?color=%23666" alt="" class="probe-page-logo probe-page-logo-mdi">

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

| Parameter | Required | Default | Description |
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
