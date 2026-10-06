# Changelog

All notable changes to the `senhub.agent` collection. The collection follows
semantic versioning, independently of the agent's own version.

## 0.1.0

First release. Role `senhub.agent.agent`:

- Linux install from the signed APT, YUM/DNF and Zypper repositories at
  `packages.senhub.io`, with the repository key checked against its
  fingerprint before it is trusted; edition (`oss` or `full`), channel
  (`stable` or `beta`) and version pinning.
- Windows install of the MSI found through the release manifest, refused
  unless the release is complete, with its SHA-256 checked before the
  install; MSI properties for the port, tags, OTLP and Zabbix.
- Licence written to `license.jwt` from a vault variable or a file.
- Multi-file configuration (`probes.d`, `strategies.d`) generated from
  `senhub_agent_probes` and `senhub_agent_strategies`, staged and checked
  with `senhub-agent config check --json` before it replaces the live files.
- `senhub_agent_state: absent` removes the agent.
- Molecule scenario (Debian 12, Ubuntu 24.04, Rocky Linux 9) and
  `deploy/ansible/test.sh`, which runs the same converge, idempotence and
  verify steps with plain `ansible-playbook`.
- The Windows tasks are written but have not been run against a Windows host.
