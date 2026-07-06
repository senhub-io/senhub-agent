# Next — 0.5.0 (unreleased)

:material-progress-clock: In progress — these changes will ship as the **0.5.0** stable release.

:material-calendar: 2026-07 · security hardening, secret store, signed Windows MSI

Three lines of work land together. An **audit-360** hardening batch
closes a set of findings across the config connectivity test (SSRF
guard, agent-key auth, bounded bodies and timeouts), config redaction
before logging, and several probe/output correctness gaps. A new
**secret store** keeps passwords and tokens out of the config: a
`${secret:}` resolver backed by OS-native storage (age / systemd-creds /
DPAPI), an `agent secret` command family, and `config_version 3` that
auto-seals inline secrets on first boot. The **Windows MSI** becomes a
guided, code-signed installer with unattended provisioning, adoption of
an existing agent, and MSI-managed auto-update.

!!! warning "Action may be required"
    **Inline secrets are auto-sealed on first boot.** The first time
    0.5.0 starts, any inline plaintext secret still present in your
    configuration is moved into the OS-native secret store, replaced by
    a `${secret:...}` reference, and the file is stamped
    `config_version: 3`. This is automatic and non-fatal (a sealing
    fault restores its own backups and the agent keeps running), but it
    **rewrites the config on disk**. A secret-free config stays at
    version 2 and is untouched.

    Two consequences to plan for:

    - **Do not downgrade under a sealed config.** An older agent
      (0.4.x and earlier, maximum supported `config_version` 2) refuses
      a version 3 config rather than pass an unresolved `${secret:}`
      literal to a probe. Upgrade every agent before distributing a
      sealed config.
    - **Windows MSI installs now auto-update by applying a new MSI**,
      not by swapping the binary in place. Fleets driven by the MSI
      should expect the installer-based update path. See the Windows MSI
      deployment guide.

<div class="rn-filter"></div>


## New

<ul class="rn">
<li><span class="tag t-new">New</span> <span class="tag t-area">secret</span> A <strong>secret store</strong> resolves <code>${secret:NAME}</code> references at config load time so passwords and tokens never sit in plaintext in <code>agent.yaml</code>, <code>probes.d/*.yaml</code> or <code>strategies.d/*.yaml</code>. A missing required reference aborts boot with the offending name (never its value); the store is only created on disk when a <code>${secret:}</code> reference actually exists. (#606)</li>
<li><span class="tag t-new">New</span> <span class="tag t-area">secret</span> OS-native backends selected automatically per platform: <code>age-keyfile</code> on Linux/macOS, <code>systemd-creds</code> as the hardened systemd opt-in, and <code>dpapi</code> on Windows. Backends initialize lazily and store their data next to the agent configuration. (#606)</li>
<li><span class="tag t-new">New</span> <span class="tag t-area">CLI</span> <code>agent secret set/get/list/rm/status</code> (plus <code>migrate</code> and, on systemd, <code>wire-unit</code>) manage the store. A secret VALUE is never accepted on the command line — <code>set</code> reads from a hidden prompt, stdin, or <code>--from-file</code>. (#606)</li>
<li><span class="tag t-new">New</span> <span class="tag t-area">config</span> <code>config_version 3</code> (Secret References): on boot the agent seals inline plaintext secrets into the store and rewrites them as <code>${secret:...}</code>. The bump to 3 happens only when a secret is actually sealed; a secret-free version 2 config stays version 2 and loads unchanged. (#606)</li>
<li><span class="tag t-new">New</span> <span class="tag t-area">config</span> The agent authentication key can be sealed into the store, with <code>agent key show</code> to reveal it behind the privilege gate. (#606)</li>
<li><span class="tag t-new">New</span> <span class="tag t-area">packaging</span> A <strong>guided Windows MSI installer</strong> (WiX 5.0.2) with license, tags and OTLP endpoint properties, and <code>config init</code> for unattended installs that seeds and provisions the configuration (including an OTLP push endpoint) without an interactive step. (#607)</li>
<li><span class="tag t-new">New</span> <span class="tag t-area">packaging</span> <strong>Code-signed</strong> MSI, executable and PowerShell payload, signed with an HSM-backed Certum certificate via jsign. (#607)</li>
<li><span class="tag t-new">New</span> <span class="tag t-area">packaging</span> The installer <strong>detects an existing agent and supports ADOPT migration</strong>, taking over an already-installed instance instead of failing or duplicating it. (#607)</li>
<li><span class="tag t-new">New</span> <span class="tag t-area">Veeam</span> The Veeam job <strong>running-duration</strong> metric is now mapped (OTel and PRTG). (#602)</li>
</ul>

## Improved

<ul class="rn">
<li><span class="tag t-improved">Improved</span> <span class="tag t-area">auto-update</span> On MSI-managed Windows installs, auto-update <strong>applies a new MSI</strong> rather than swapping the binary, so the packaged install stays consistent with its installer state. (#607)</li>
<li><span class="tag t-improved">Improved</span> <span class="tag t-area">CLI</span> Destructive actions (uninstall, <code>secret rm</code>, license remove) now confirm with a unified <code>[y/N]</code> prompt; a bypass flag skips it for unattended runs (<code>--yes</code> for uninstall and <code>secret rm</code>, <code>--force</code> for license remove). (#609)</li>
<li><span class="tag t-improved">Improved</span> <span class="tag t-area">CLI</span> <code>--help</code> surfaces the <code>secret</code>, <code>key</code>, <code>db-monitoring</code> and <code>config init</code> commands. (#609)</li>
</ul>

## Fixed

<ul class="rn">
<li><span class="tag t-security">Security</span> <span class="tag t-area">HTTP</span> The config connectivity test (<code>validate</code>/<code>preview</code>/<code>test</code>) now <strong>blocks link-local and metadata targets</strong> (SSRF guard), <strong>requires agent-key auth</strong>, <strong>bounds the request body size</strong>, and <strong>bounds outbound HTTP client timeouts</strong>. (#609)</li>
<li><span class="tag t-security">Security</span> <span class="tag t-area">config</span> Resolved config parameters are <strong>redacted before logging</strong>, so secrets substituted into the config never reach the logs. (#609)</li>
<li><span class="tag t-security">Security</span> <span class="tag t-area">OTLP</span> The OTLP strategy <strong>fails fast on a blank auth token</strong> instead of silently sending unauthenticated. (#609)</li>
<li><span class="tag t-security">Security</span> <span class="tag t-area">packaging</span> The MSI <strong>masks <code>LICENSE_KEY</code></strong> in the install log and stages the update MSI in a private per-run directory instead of shared temp (TOCTOU hardening). (#607, #609)</li>
<li><span class="tag t-security">Security</span> <span class="tag t-area">ci</span> GitHub Actions are pinned to commit SHA. (#609)</li>
<li><span class="tag t-fixed">Fixed</span> <span class="tag t-area">otlp_receiver</span> Re-exported metrics <strong>preserve their instrument type and unit</strong>, instead of being flattened on the way back out. (#609)</li>
<li><span class="tag t-fixed">Fixed</span> <span class="tag t-area">senhub</span> A permanent <code>4xx</code> response now <strong>drops the payload</strong> instead of retrying it forever; transient auth failures (<code>401</code>/<code>403</code>) are retried rather than dropped, so an intake auth blip does not lose data. (#609, #613)</li>
<li><span class="tag t-fixed">Fixed</span> <span class="tag t-area">linux_logs</span> Death of the <code>journalctl</code> subprocess is detected and the reader is <strong>respawned</strong> with a bounded backoff, so log collection resumes without an agent restart. (#609, #613)</li>
<li><span class="tag t-fixed">Fixed</span> <span class="tag t-area">logicaldisk</span> All real filesystems are collected via a blocklist, instead of an allowlist that dropped legitimate mounts. (#609)</li>
<li><span class="tag t-fixed">Fixed</span> <span class="tag t-area">entity</span> Locally-monitored probe targets are anchored to their host with <code>runs_on</code>, so a local target no longer surfaces as an isolated entity. (#601)</li>
<li><span class="tag t-fixed">Fixed</span> <span class="tag t-area">CLI</span> <code>db-monitoring</code> no longer requires administrator on Windows. (#609)</li>
<li><span class="tag t-fixed">Fixed</span> <span class="tag t-area">CLI</span> <code>agent update --help</code> no longer tries to install a release named <code>--help</code>. (#609)</li>
<li><span class="tag t-fixed">Fixed</span> <span class="tag t-area">auto-update</span> The release list is revalidated with no-cache, so a freshly published stable is seen at once. (#599)</li>
</ul>

## Changed

<ul class="rn">
<li><span class="tag t-removed">Changed</span> <span class="tag t-area">CLI</span> Decorative emoji are removed from CLI output, fatal errors print a plain <code>Error</code> line, and diagnostic errors and warnings are routed to <strong>stderr</strong> — friendlier for scripts and log capture. (#609)</li>
<li><span class="tag t-breaking">Breaking</span> <span class="tag t-area">Prometheus</span> OTLP-ingested cumulative counters are now exported as counters, so the Prometheus endpoint appends the OpenMetrics <code>_total</code> suffix (for example <code>http_server_requests</code> becomes <code>http_server_requests_total</code>). This is the correct OpenMetrics naming, but <strong>scrape-visible series names change on upgrade</strong> — update any dashboards or alerts that match the old names. (#613)</li>
<li><span class="tag t-removed">Changed</span> <span class="tag t-area">secret</span> The secret store returns stored values <strong>exactly as written</strong> (no whitespace trimming), so a secret with meaningful leading or trailing bytes round-trips unchanged. (#613)</li>
</ul>

## Internal

<ul class="rn">
<li><span class="tag t-internal">Internal</span> <span class="tag t-area">license</span> The license field is edited at the YAML node level rather than by re-marshalling the whole config, preserving the rest of the file untouched. (#609)</li>
<li><span class="tag t-internal">Internal</span> <span class="tag t-area">config</span> Generated config comments trimmed; the config-check version derives from <code>CurrentConfigVersion</code>; the show load path records the config directory. (#609)</li>
<li><span class="tag t-internal">Internal</span> <span class="tag t-area">packaging</span> A local Certum signing helper (<code>sign-release-msi.sh</code>) and an updated MSI guide document the release-time signing flow; the MSI CI workflow is dev/test-only (manual dispatch). (#607, #610)</li>
</ul>
