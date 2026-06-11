# Zabbix Integration Study — protocol options and decision

Status: **decided 2026-06-11** (maintainer arbitrage). Supersedes the
original #169 scope (HTTP endpoint + parsing template).

## Decision

1. **The HTTP-endpoint-plus-template approach is abandoned.** It is the
   operationally weakest option: every host added by hand, a template to
   maintain on every metric change, polling load on the Zabbix server,
   and no auto-creation of hosts — structurally.
2. **The target design is a native Zabbix ACTIVE agent mode** (outbound
   to server/proxy port 10051): autoregistration (hosts appear by
   themselves, an autoregistration action links groups + templates),
   batched value push, heartbeat, and LLD discovery keys mapped from the
   transformer `multi_instance_labels`. Templates are GENERATED from the
   transformer YAML definitions (single source of truth), one export per
   supported LTS line.
3. **The value proposition is the vendor-depth probes, not generic host
   monitoring.** zabbix_agent2 is the reference for host metrics; we do
   not compete with it. What Zabbix lacks and we have: IBM i, Veeam,
   NetScaler/Citrix, Redfish depth — paid probes, i.e. a sales channel
   into Zabbix shops, plus the dual-run migration story (same agent
   feeds Zabbix and the modern stack simultaneously).
4. **Scheduling: deferred until a Zabbix prospect materializes.** The
   current pipeline is PRTG-shaped; #156 (snmp_poll production-grade)
   serves demand we already have. This study keeps the design ready to
   draw. Estimated effort when triggered: ~2 weeks (lots below).
5. Interim hygiene: the routed-and-advertised 501 stub endpoint is
   removed (an advertised 501 reads as a broken deployment).

### Planned lots (when triggered)

- **Lot 1 (~4-6 d):** active agent core — ZBXD framing, `active checks`
  request with `HostMetadata`, autoregistration, batched `agent data`
  push, heartbeat. 6.0-dialect subset (works against 6.0 LTS -> 8.x).
- **Lot 2 (~3-4 d):** LLD discovery keys per dynamic dimension
  (filesystems, interfaces, check targets, vendor objects) + template
  generation from transformer YAML (items + LLD prototypes), 6.0 and
  7.0 exports + autoregistration action documentation.
- **Lot 3 (optional, ~2 d):** passive listener on 10050, pre-7.0
  plaintext dialect (universal thanks to server fallback), `agent.ping`
  for the availability icon.
- **TLS:** certificate TLS via Go stdlib. PSK is NOT implemented (no
  RFC 4279 support in crypto/tls — golang/go#6379); consequence:
  encrypted autoregistration (PSK-only in Zabbix) is unavailable —
  document unencrypted autoreg or a local proxy as the patterns.
- **License guardrail:** Zabbix server/agent2 source is GPL-2.0 (<=6.4)
  / AGPL-3.0 (>=7.0). Never port code from it. The protocol is publicly
  documented and freely implementable; the MIT third-party references
  below are safe.

The full research brief follows, unedited.

---

# Technical Brief: Integration Paths for a Third-Party Go Agent with Zabbix

Audience: SenHub agent maintainers. Scope: every protocol-level mechanism by which our Go binary could feed Zabbix, with version caveats (6.0 LTS / 7.0 LTS / 7.2+/8.0-dev), encryption constraints, and licensing of any code we might read. No recommendation is made; trade-offs are at the end.

---

## 0. Common wire envelope (all Zabbix TCP protocols)

Every Zabbix TCP exchange (passive, active, sender/trapper) is wrapped in the same header ([protocol header docs](https://www.zabbix.com/documentation/7.0/en/manual/appendix/protocols/header_datalen)):

| Field | Size | Content |
|---|---|---|
| Protocol | 4 B | `"ZBXD"` (`5A 42 58 44`) |
| Flags | 1 B | `0x01` Zabbix communications protocol, `0x02` compression (zlib), `0x04` large packet |
| Data length | 4 B (8 B if large) | little-endian; compressed size if compressed |
| Reserved | 4 B (8 B if large) | uncompressed size when compression used, else zeros |

Reference construction: `b"ZBXD\x01" + struct.pack("<II", len(data), 0) + data`. Limits: 1 GB standard packet, 16 GB with the `0x04` large-packet flag. This envelope is trivial in Go (a dozen lines with `encoding/binary`); several MIT-licensed Go implementations exist (section 8).

---

## 1. Passive agent protocol (server -> agent, TCP 10050)

**What it is.** Zabbix server/proxy pollers connect to the agent's listen port per item, per interval.

**Two wire dialects:**

- **Pre-7.0 plaintext** ([6.0 docs](https://www.zabbix.com/documentation/6.0/en/manual/appendix/items/activepassive)): server sends the bare item key (e.g. `agent.ping\n`); agent replies `ZBXD`-framed value (`1` for `agent.ping`). Unsupported key -> `ZBX_NOTSUPPORTED\0<error text>` (literal string, NUL, human-readable reason).
- **7.0+ JSON** ([7.0 docs](https://www.zabbix.com/documentation/7.0/en/manual/appendix/items/activepassive)): server sends `{"request":"passive checks","data":[{"key":"agent.version","timeout":3}]}`; agent replies `{"version":"7.0.0","variant":2,"data":[{"value":"7.0.0"}]}`. Errors per item: `"data":[{"error":"Unsupported item key."}]`. `variant` is 1 = agent, 2 = agent 2.
- **Failover**: a 7.0 server first tries JSON; if the agent answers `ZBX_NOTSUPPORTED` to the JSON blob, the server caches the interface as old-protocol and retries plaintext, re-probing JSON hourly ([docs](https://www.zabbix.com/documentation/current/en/manual/appendix/items/activepassive)). **Consequence: a passive Go agent that only implements the pre-7.0 plaintext dialect works against 6.0, 7.0 and 8.x servers unchanged**, at the cost of one failed JSON probe per hour per interface.

**Minimum viable passive agent**: accept TCP, parse one key per connection (plaintext) or the JSON batch (7.0), answer `agent.ping`->`1` so the `Zabbix agent` availability icon goes green, answer your custom keys (`senhub.cpu.util[...]` etc.), `ZBX_NOTSUPPORTED\0...` for everything else, close connection. Item keys are free-form `name[param1,param2]` strings; the server just sends whatever the template says.

**TLS**: passive connections may be unencrypted, certificate, or PSK, controlled host-side by `TLSAccept` on the agent and per-host "Connections to host" in the frontend ([encryption docs](https://www.zabbix.com/documentation/7.0/en/manual/encryption)). PSK is **not mandatory** — certificate TLS or no TLS are equally valid choices per host. See section 9 for the Go PSK gap.

---

## 2. Active agent protocol (agent -> server/proxy, TCP 10051)

**Check list request** — agent connects and sends ([7.0 docs](https://www.zabbix.com/documentation/7.0/en/manual/appendix/items/activepassive)):

```json
{"request":"active checks","host":"<Hostname>","host_metadata":"mysql,nginx",
 "interface":"zabbix.server.lan","ip":"159.168.1.1","port":12050,
 "version":"7.0.0","variant":2,"config_revision":1,"session":"e3dcbd9a..."}
```

Server responds with the item list: `{"response":"success","config_revision":2,"data":[{"key":"system.uptime","itemid":1234,"delay":"10s","lastlogsize":0,"mtime":0}]}`. In 6.0 the request has no `version/variant/config_revision/session` fields and the response includes `key_orig` ([6.0 docs](https://www.zabbix.com/documentation/6.0/en/manual/appendix/items/activepassive)). Since 6.4 sync is **incremental every 5 s** via `config_revision` (omitted `data` = unchanged) instead of a full list every 2 min. 7.0 adds remote `commands` in both directions (`"wait":0/1`).

**Value push** — `{"request":"agent data","session":"...","host":"...","data":[{"id":1,"itemid":5678,"value":"...","clock":1712830783,"ns":76808644}]}` (7.0; in 6.0 each datum carries `host`+`key` instead of `itemid`). Values are batched in one connection; server acks `{"response":"success","info":"processed: 2; failed: 0; total: 2; seconds spent: 0.003534"}`. Not-supported items are reported with `"state":1` and the error text as `value` ([current docs](https://www.zabbix.com/documentation/current/en/manual/appendix/items/activepassive)).

**Heartbeat** (since 6.2, [ZBX-21356](https://support.zabbix.com/browse/ZBX-21356)): `{"request":"active check heartbeat","host":"...","heartbeat_freq":60}` every `HeartbeatFrequency` seconds; active checks deemed unavailable after 2x that.

**Host autoregistration** ([7.0 docs](https://www.zabbix.com/documentation/7.0/en/manual/discovery/auto_registration)) — the killer feature of active mode: there is **no separate registration message**. When an unknown `host` sends `active checks`, an autoregistration event fires server-side. The agent contributes:
- `host` (Hostname; comma-delimited = multiple host registrations),
- `host_metadata` — from `HostMetadata` config (max **2034 bytes**, [agentd config](https://www.zabbix.com/documentation/current/en/manual/appendix/config/zabbix_agentd)) or `HostMetadataItem` (up to 65535 UTF-8 code points, truncated beyond),
- `interface`/`ip`/`port` (`HostInterface[Item]`, `ListenIP`, `ListenPort`) — used by the server to create the host's passive interface (defaults: source IP, port 10050).

Server-side, an **Autoregistration action** matches on hostname/metadata (substring/regex) and can *create the host, add it to host groups, and link templates* — fully zero-touch. Re-registration fires when metadata changes, so a Go agent can change templates dynamically by changing the metadata string. Caveat: **autoregistration encryption supports only "No encryption" and/or "PSK"** (Administration -> General -> Autoregistration) — certificate TLS is not an autoregistration option, which makes the Go PSK gap (section 9) load-bearing if encrypted autoregistration is required.

A third-party active agent does **not** need to implement remote commands, log checks (`lastlogsize`/`mtime`) or incremental sync to be functional; the 6.0-level subset (request list, push values, heartbeat) works against 6.0->8.x servers, since servers accept agents back to 4.4/1.4 but **never newer than the server's major version** ([compatibility](https://www.zabbix.com/documentation/7.0/en/manual/appendix/compatibility)) — so the `version` field we advertise should be conservative.

---

## 3. Low-Level Discovery (LLD)

[LLD docs](https://www.zabbix.com/documentation/7.0/en/manual/discovery/low_level_discovery). A discovery *rule* is itself an item (any type: passive agent, active agent, trapper, HTTP agent, dependent) whose value is a JSON array of macro objects:

```json
[{"{#FSNAME}":"/","{#FSTYPE}":"ext4"},{"{#FSNAME}":"/data","{#FSTYPE}":"xfs"}]
```

Since 4.2 the root is a bare array (legacy `{"data":[...]}` is still auto-extracted via `$.data`). Native agents serve discovery keys like `vfs.fs.discovery` exactly this way — the key returns the JSON, the server expands **prototypes** (items, triggers, graphs, even hosts) by substituting `{#MACRO}` ([custom LLD rules](https://www.zabbix.com/documentation/current/en/manual/discovery/low_level_discovery/custom_rules)). So a Go agent gets dynamic per-disk/per-interface/per-probe item creation by exposing one key per discovery dimension (e.g. `senhub.probe.discovery` -> `[{"{#PROBE}":"mysql"},...]`) plus prototype items `senhub.metric[{#PROBE},...]` in the shipped template.

Interaction with modes: works identically whether the rule is passive, active, or **trapper** (you can `zabbix_sender` the discovery JSON into a trapper-type rule). Frequency is the rule's update interval (operator-controlled; discovery is cheap server-side but creates DB churn — typical practice is 30 m-1 h). Size limits ([notes](https://www.zabbix.com/documentation/current/en/manual/discovery/low_level_discovery/notes)): no limit when received directly by server; 16 MB through a user parameter; DB-bound through a proxy. Dependent rules can't chain off other discovery rules; manually disabled discovered entities stay disabled.

---

## 4. Trapper / zabbix_sender protocol (push, TCP 10051)

[Sender protocol](https://www.zabbix.com/documentation/7.0/en/manual/appendix/protocols/zabbix_sender). Same `ZBXD` envelope; payload:

```json
{"request":"sender data","data":[
  {"host":"Host 1","key":"senhub.cpu.util","value":"42.1","clock":1712830783,"ns":76808644}]}
```

Response: `{"response":"success","info":"processed: 1; failed: 0; total: 1; seconds spent: 0.060753"}`. Arbitrary batching across hosts/keys in one packet; optional `clock`/`ns` for historical backfill.

Constraints: target **host and items must pre-exist** (item type *Zabbix trapper*, or *HTTP agent* with "Allow trapping" enabled — [HTTP item docs](https://www.zabbix.com/documentation/7.0/en/manual/config/items/itemtypes/http)); trapper items have an "Allowed hosts" ACL (CIDR/DNS). No autoregistration on this path — but it composes with section 2: an agent can autoregister via one `active checks` request, let the template create trapper items, then push everything via sender packets. Proxies fully relay sender data for hosts they monitor. TLS cert **or** PSK both supported (zabbix_sender is an explicitly listed encryption client, [encryption docs](https://www.zabbix.com/documentation/7.0/en/manual/encryption)).

---

## 5. Zabbix Agent 2 plugin framework

Agent 2 is itself Go. Two plugin kinds ([plugins doc](https://www.zabbix.com/documentation/current/en/manual/extensions/plugins)):
- **Built-in** — compiled into agent 2; requires building inside the Zabbix tree. Not viable for us.
- **Loadable** (since 6.0, [guidelines](https://www.zabbix.com/documentation/guidelines/en/plugins/loadable_plugins)) — a **standalone binary** that agent 2 launches and talks to **bidirectionally over Unix sockets (Linux) / named pipes (Windows)**, configured via `Plugins.<Name>.System.Path=/path/to/binary`.

Protocol (from [`golang.zabbix.com/sdk/plugin/comms`](https://pkg.go.dev/golang.zabbix.com/sdk/plugin/comms)): JSON messages with `{"id","type"}` header; 12 message types — `Register` (handshake, declares `Name`, `Metrics`, interface flags), `Validate`/`Configure`, `Start`/`Terminate`, `Export` (key+params+timeout -> value), `Log`, `Period`. `ProtocolVersion = "6.4.0"`. Loadable plugins may implement **Exporter, Runner, Configurator only — Watcher and Collector interfaces are not supported**, and Exporter loses ContextProvider.

So yes, a third-party binary *can* register as an agent2 plugin — our binary could expose a thin plugin shim that answers `Export` requests from our in-process data store. Constraints: the binary's lifecycle is owned by agent2 (started/terminated by it); the metric set is whatever `RegisterMetrics()` declared; the customer must already run Zabbix agent 2 and edit its config; SDK guidance is to build against the matching agent branch ([guidelines](https://www.zabbix.com/documentation/guidelines/en/plugins/loadable_plugins)). **Licensing is clean for this path**: the plugin SDK `golang.zabbix.com/sdk` is **MIT** (copyright Zabbix SIA, [pkg.go.dev license](https://pkg.go.dev/golang.zabbix.com/sdk?tab=licenses)) — importable in our Apache-2.0 repo. Distribution friction is high, though: it's a sidecar inside someone else's agent, and metrics flow through agent2's scheduling/active-passive machinery, not ours.

---

## 6. HTTP agent item (the abandoned original spec)

[HTTP item docs](https://www.zabbix.com/documentation/7.0/en/manual/config/items/itemtypes/http). Zabbix **server (or proxy)** polls an HTTP/S URL; no agent protocol involved. Typical pattern: one **master item** fetches our JSON blob, then **dependent items** with JSONPath preprocessing fan it out. Capabilities: GET/POST/PUT/HEAD, Basic/NTLM/Kerberos/Digest auth, full TLS with client certs, 1-600 s timeout, async pollers (`StartHTTPAgentPollers`, max concurrency 1000), "Allow trapping" turns the same item into a push target.

Fair statement of limits:
- **No autoregistration**: every monitored host is manual frontend/API work, or needs network discovery — the single biggest operational gap vs an active agent for an MSP fleet.
- **Template maintenance**: every new SenHub metric needs a dependent-item prototype + JSONPath in the template; metric renames break customers' templates. LLD *can* be driven from the JSON (discovery rule = dependent item on the master), which softens but doesn't remove this.
- **Polling load sits on the Zabbix server/proxy**, scaling with host count x interval; it is pull-only and interval-quantized (no event-driven push).
- **Dependent item limits**: <= 29999 dependent items per master and 3 dependency levels up to 7.0; **both limits removed in 7.2** ([ZBXNEXT-9233](https://support.zabbix.com/si/jira.issueviews:issue-html/ZBXNEXT-9233/ZBXNEXT-9233.html), [dependent items doc](https://www.zabbix.com/documentation/current/en/manual/config/items/itemtypes/dependent_items)) — irrelevant for 6.0/7.0 LTS users until they upgrade. Large (>=1 MiB) JSON master values need DB/cache tuning ([large JSON values](https://www.zabbix.com/documentation/devel/en/manual/appendix/items/large_json_values)).

---

## 7. Templates (how a vendor ships one)

[Template export/import](https://www.zabbix.com/documentation/7.0/en/manual/xml_export_import/templates). Formats: **YAML (default), XML, JSON**. Structure: `zabbix_export: {version: '7.0', template_groups, templates: [items, discovery_rules (+ item/trigger prototypes), dashboards, value_maps, macros, tags, ...]}`. Import rules (create new / update existing / delete missing) are operator-controlled. Compatibility: a 7.0 server imports files "not older than version 1.8" ([compatibility](https://www.zabbix.com/documentation/7.0/en/manual/appendix/compatibility)) — i.e. imports are backward-compatible but **not forward**: a file exported with `version: '7.0'` won't import into a 6.0 server, so we'd ship per-LTS template files (a 6.0 export imports fine into 7.0+). Whatever integration mode we choose, the deliverable to customers is one template (+ autoregistration action description, if active mode) per LTS line.

---

## 8. Existing Go implementations to learn from (licenses are load-bearing — our repo is Apache-2.0)

| Code | What | License | Notes |
|---|---|---|---|
| Zabbix agent 2 (`src/go/` in [zabbix/zabbix](https://github.com/zabbix/zabbix), entry `src/go/cmd/zabbix_agent2/`) | Full reference Go implementation of both agent protocols | **GPL-2.0 through 6.4; AGPL-3.0 from 7.0** ([Zabbix license page](https://www.zabbix.com/license), [official blog](https://blog.zabbix.com/striking-the-right-balance-zabbix-7-0-to-be-released-under-agplv3-license/27596/)) | **Do not port code from it** — AGPL (or GPLv2) contamination into Apache-2.0. Reading for protocol understanding is fine; the protocol itself is documented and freely implementable. |
| [`golang.zabbix.com/sdk`](https://pkg.go.dev/golang.zabbix.com/sdk?tab=licenses) | Official agent2 plugin SDK (plugin, comms, conf, tlsconfig pkgs) | **MIT** | Safe to import. Only path that *requires* it is section 5. |
| [ecnepsnai/zbx](https://github.com/ecnepsnai/zbx) | Third-party Go implementation of passive **and** active agent (callback-based custom keys), "Zabbix 4+" | **MIT** | Proof the agent protocols are small; usable as reference or dependency. Predates 7.0 JSON passive dialect. |
| [adubkov/go-zabbix](https://github.com/adubkov/go-zabbix), [datadope-io/go-zabbix](https://github.com/datadope-io) (maintained fork), [chmller/go-zabbix-sender](https://pkg.go.dev/github.com/chmller/go-zabbix-sender), [AlekSi/zabbix-sender](https://github.com/AlekSi/zabbix-sender) | Sender/trapper protocol clients | MIT-family (verify the exact fork chosen at adoption time) | The sender protocol is ~100 LOC; writing our own removes the dependency question entirely. |
| [akomic/go-zabbix-proto](https://github.com/akomic/go-zabbix-proto) | Sender + agent protocol structs | check repo | reference only |

---

## 9. Encryption and the Go TLS-PSK gap

Zabbix encryption ([docs](https://www.zabbix.com/documentation/7.0/en/manual/encryption)): TLS 1.2/1.3, **certificate or PSK**, per-component and per-host optional (unencrypted by default). PSK details ([PSK docs](https://www.zabbix.com/documentation/7.0/en/manual/encryption/using_pre_shared_keys)): identity = UTF-8 string <=128 chars sent in clear; key = hex string, **min 128-bit (32 hex digits), max 2048-bit**; TLS 1.2 ciphersuites of the `(ECDHE-)PSK-AES128-*` family (RFC 4279/5489).

**The gap**: Go's `crypto/tls` has **no external/RFC 4279 PSK support** — [golang/go#6379](https://github.com/golang/go/issues/6379) is open since 2013, milestone "Unplanned"; TLS 1.3 PSK exists only as session-resumption via `ClientSessionCache`, not operator-provisioned keys. Options if we need PSK: (a) cgo bindings to OpenSSL (deployment cost, breaks pure-Go cross-compile of our 5-platform matrix), (b) a maintained fork/patch of `crypto/tls` (security-maintenance burden), (c) **don't do PSK**: support certificate TLS (pure stdlib) + unencrypted, and document that *encrypted autoregistration* is the one Zabbix feature that PSK-only-ness blocks (section 2 — autoregistration accepts only no-encryption or PSK). PSK is never mandatory for passive/active/sender per se; it is only mandatory **if** the customer requires encryption **and** autoregistration on the same connection, or mandates PSK by policy.

---

## 10. Compatibility matrix

| Mode | 6.0 LTS | 7.0 LTS | 7.2+/8.x | Via proxy | Encryption |
|---|---|---|---|---|---|
| Passive plaintext | yes | yes (auto-fallback) | yes (fallback retained) | yes | none/cert/PSK |
| Passive JSON | no (7.0+ servers only) | yes | yes | yes | none/cert/PSK |
| Active checks + autoregistration | yes (pre-6.4 full list; 6.0 fields) | yes (incl. incremental sync, commands) | yes | yes | none/PSK for autoreg; none/cert/PSK for data |
| Trapper / sender | yes | yes | yes | yes (host must be proxy-monitored) | none/cert/PSK |
| Agent2 loadable plugin | yes (6.0+) | yes | yes (build per branch) | n/a (rides agent2) | agent2's own TLS |
| HTTP agent item | yes | yes | yes (dependent-item limits lifted in 7.2) | yes (proxy executes) | HTTPS (standard) |

Server accepts agents *older* than itself (agentd back to 1.4, agent2 back to 4.4) but **not newer** ([compatibility](https://www.zabbix.com/documentation/7.0/en/manual/appendix/compatibility)); a third-party agent advertising a low `version` string is compatible across the board.

---

## 11. Trade-off table (Go binary that already has all metrics in-process; MSP migrating off PRTG)

| Criterion | Passive agent (10050) | Active agent (10051) | Trapper/sender | Active+trapper hybrid | Agent2 loadable plugin | HTTP agent item |
|---|---|---|---|---|---|---|
| Host auto-creation | no (manual/API/netdiscovery) | **yes — autoregistration + action-driven template link** | no (host must pre-exist) | **yes** (register via active, push via sender) | no (host = the agent2 host, manual) | no |
| Dynamic discovery (LLD) | yes (discovery keys) | yes (discovery keys) | yes (push JSON to trapper rule) | yes | yes | yes (dependent-item LLD) |
| Push vs pull | pull (server polls each item) | **push** (batched, interval from server config) | **push** (fully agent-paced) | push | pull-through-agent2 | pull (server-paced) |
| Encryption | cert (stdlib) / PSK (gap section 9) | data: cert or PSK; **autoreg: PSK-only if encrypted** | cert or PSK | same as active | agent2 handles TLS | HTTPS, stdlib, no gap |
| Implementation effort in our binary | low | medium | **lowest** (~100 LOC client) | medium | medium (SDK shim) + ops friction | zero agent code but template-heavy |
| Server-side load | poller per item per host | light (trapper processes) | light | light | poller->agent2 | HTTP pollers per host; dependent preprocessing |
| MSP friction (PRTG exit) | inbound 10050 to every host; manual host creation | **lowest**: outbound-only, hosts appear by themselves, one template + one autoreg action | host onboarding manual | lowest overall | requires deploying agent2 everywhere — defeats single-binary story | every host by hand; template upkeep; server polling scales poorly |
| Hard blockers / risks | don't port agent2 code (GPL/AGPL) | TLS-PSK gap iff encrypted autoreg required | items must pre-exist | same PSK caveat | sidecar lifecycle; per-branch builds; SDK MIT (safe) | dependent-item caps until 7.2; no autoreg, structurally |
