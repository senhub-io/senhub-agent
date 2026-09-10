<img src="https://api.iconify.design/mdi/clock-check-outline.svg?color=%23666" alt="" class="probe-page-logo probe-page-logo-mdi">

!!! info
    **License: Free** — part of the universal collection tier.

# NTP (direct measurement)

The `ntp` probe measures how wrong the local clock is, by exchanging NTP packets
with reference servers you name. It reports the offset between this machine's
clock and the reference, the delay of the exchange that produced that offset,
and what the reference says about itself.

It does not read any local time daemon and does not need one. chrony, ntpd,
systemd-timesyncd and the Windows Time service each report their own view of
synchronisation, in their own format, through their own tool — and a host
running none of them reports nothing at all. This probe asks the same question
on every platform: does this machine's clock agree with a reference, and by how
much.

Works on Linux, Windows and macOS. Requires no software on the host and no
elevated privileges, but does require outbound UDP 123 to the servers you name.

## Quick start

```yaml
# probes.d/10-ntp.yaml — each file under probes.d/ is a YAML array of probes
- name: ntp
  type: ntp
  params:
    servers:
      - ntp1.example.internal
      - ntp2.example.internal
```

There is no default server, on purpose. See [Choosing servers](#choosing-servers).

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->

| Parameter | Required | Default | Description |
|---|---|---|---|
| `servers` | Yes | - | Reference servers as host or host:port; port 123 otherwise. Example: `ntp.example.org, 10.0.0.1:123` |
| `samples` | No | `4` | Exchanges per server per cycle, the least delayed is kept; at most 16 |
| `timeout` | No | `5` | Per-exchange timeout in seconds |
| `interval` | No | `300` | Seconds between cycles; every query is traffic to somebody else's server |

<!-- schema:params:end -->

| Parameter | Type | Default | Description |
|---|---|---|---|
| `servers` | list of strings | **required** | Reference servers to measure against. Each entry is a hostname or IP, optionally with a port (`ntp.example.org:1123`); port 123 is used otherwise. |
| `samples` | int | `4` | Exchanges per server per cycle. The least delayed one is kept. Maximum 16. |
| `timeout` | int (seconds) | `5` | Per-exchange timeout. |
| `interval` | int (seconds) | `300` | Collection interval. Longer than most probes on purpose: clock error moves slowly and every query is traffic sent to somebody else's server. |

## Metrics

Every series is tagged with the `server` it was measured against, so two
reference servers produce two independent sets.

| Metric | Unit | Description |
|---|---|---|
| `senhub.ntp.up` | 1 | 1 when the server answered with a usable measurement, 0 otherwise |
| `senhub.ntp.state` | 1 | One series per reason, exactly one of which is 1: `ok`, `unreachable`, `refused`, `unsynchronised`, `invalid_response` |
| `ntp.time.offset` | ms | Measured error of the system clock. Positive means the local clock is ahead of the reference, negative means behind. |
| `ntp.round_trip.delay` | ms | How long the measuring exchange spent in flight. This is the confidence attached to the offset beside it — see [Accuracy](#accuracy-and-its-limits). |
| `ntp.stratum` | 1 | Stratum of the reference server (1 = directly attached to a reference clock) |
| `ntp.root.delay` | ms | Delay from the reference server up to its own stratum-1 source |
| `ntp.root.dispersion` | ms | Maximum error the reference server itself accumulates relative to its stratum-1 source |
| `ntp.leap_status` | 1 | Leap indicator the server announces: 0 = normal, 1 = the last minute of the day has 61 seconds, 2 = it has 59 |

## Reading the numbers

A time offset only matters relative to what it breaks. These are the thresholds
worth building alerts on, and why:

| `ntp.time.offset` | What it means |
|---|---|
| under 10 ms | Normal for a synchronised host. Nothing to do. |
| 10 ms to 100 ms | Correlating logs or traces across hosts starts putting events in the wrong order. |
| 100 ms to 1 s | Worth alerting. A working time daemon should never let a host get here; if one is running, it is not steering the clock. |
| over 60 s | One-time passwords (TOTP) begin to fail — the usual step is 30 seconds with one step of tolerance either side. |
| over 5 minutes | Kerberos and Active Directory authentication fails outright. This is the default maximum skew those protocols accept. |

Alert on `senhub.ntp.state` as well as on the offset, not instead of it. The
common real failure is not a drifting clock — it is a firewall that closes on
outbound UDP 123, and that shows up as `unreachable` with no offset published at
all. A rule written only on the offset stays quiet through it.

Nothing is published for a failed exchange, deliberately. A fabricated offset of
0 would be indistinguishable from a perfectly synchronised clock, which is the
one reading you must never invent.

## Accuracy and its limits

Be clear about what this measurement can and cannot support.

NTP derives the offset from four timestamps and assumes the request and the
reply spent the same amount of time in flight. When the path is asymmetric — a
congested uplink, an asymmetric route, a busy hypervisor — the error in the
offset is half of however asymmetric it was, and **that error cannot be detected
from inside a single exchange**.

The probe reduces it the way NTP clients have always reduced it: take several
exchanges and keep the one with the smallest round trip, because queuing is what
makes a path asymmetric and the least delayed exchange queued the least. This
lowers the error. It does not measure it.

That is why `ntp.round_trip.delay` is published next to every offset instead of
being discarded. Read them together:

- **Round trip under 1 ms** (same LAN): trust the offset to well under a
  millisecond.
- **Round trip of 20 to 50 ms** (a server across the internet): trust the offset
  to a few milliseconds.
- **A round trip that is large, or that jumps between cycles**: the offset next
  to it is coarse, and a change in it may be the network moving rather than the
  clock.

So: this probe reliably answers "is this clock wrong enough to break something",
which is a question asked at the scale of tens of milliseconds and up. It is not
a precision instrument, and it should not be used to chase sub-millisecond
accuracy or to discipline anything.

## Choosing servers

**Name the servers this host is actually supposed to follow.** Measuring against
some other reference tells you that two references disagree, which is not the
same as knowing your clock is wrong.

**Naming two is worth it.** A single reference that is itself wrong looks
exactly like a correct one. Two that agree is evidence; two that disagree tells
you to go and look.

**There is no default server on purpose.** A default would silently point every
agent that enables this probe at somebody else's infrastructure, and it would
also be the wrong measurement for most hosts.

**Public pools deserve care.** `pool.ntp.org` is run by volunteers, and its
usage policy expects a product that queries it at scale to obtain its own vendor
zone rather than hammer the general pool. The default 5-minute interval and the
16-sample cap exist for that reason. If you monitor a fleet, point it at your own
NTP servers.

**Outbound UDP 123 must be open** to each server named. Where it is filtered the
probe reports `state=unreachable`, which is a firewall finding rather than a
clock finding — and a useful one, because a host whose NTP egress has just been
blocked will keep looking perfectly synchronised for hours before it starts to
drift.

## Relationship with the chrony probe

The two are complementary and answer different questions. Running both is
worthwhile on a host that has chrony.

| | `chrony` probe | `ntp` probe |
|---|---|---|
| Source of the number | What the local daemon believes | What an independent server observes |
| Needs a time daemon | Yes, chrony specifically | No |
| Needs configuration | No | Yes, at least one server |
| Needs outbound UDP 123 | No | Yes |
| Reports drift rate and skew | Yes | No |
| Detects a daemon synced to a wrong source | No | Yes |

The last two rows are the point of running both.

Frequency offset and skew — the rate at which the clock drifts and the
uncertainty on that rate — can only come from a daemon that has been filtering
measurements for hours. A rising skew warns that a hardware clock or a
virtualisation host is failing *before* the time itself moves.

Conversely, a daemon reports its own self-assessment. One that is synchronised
to a wrong source reports an offset near zero with complete confidence. Only an
independent measurement contradicts it: when `chrony`'s offset and `ntp`'s
offset disagree, the daemon is following something it should not be.

## Troubleshooting

Read `senhub.ntp.state` first; it names the cause directly.

| State | Meaning | What to do |
|---|---|---|
| `ok` | The exchange completed and the offset is published. | — |
| `unreachable` | Nothing came back. | Check outbound UDP 123 towards this server, and that the name resolves. This is the most common failure and it is usually a firewall. |
| `refused` | The server answered, refusing us with a kiss-o'-death code (`RATE`, `DENY`, `RSTR`). | `RATE` means the interval is too short for that server — raise `interval`. `DENY` and `RSTR` mean it does not serve us; use a server that does. The probe stops querying for the rest of the cycle rather than retrying, which is what the code asks for. |
| `unsynchronised` | The server answered but declares its own clock unsteered. | The problem is at the reference, not on this host. Its timestamps are not usable and are deliberately not published. |
| `invalid_response` | Something answered on port 123 but not with a usable NTP reply. | Check what is really listening — a captive portal or a middlebox intercepting UDP 123 produces this. |

## Operational notes

- This probe is not enabled by default and cannot be, because it requires a
  server to be named.
- It reports a property of **this** host: its clock. The server it queries is a
  reference, not a monitored system, and no entity is emitted for it. The
  measurement belongs to the host in the topology.
- The offset is signed. Positive means the local clock is ahead of the
  reference.
