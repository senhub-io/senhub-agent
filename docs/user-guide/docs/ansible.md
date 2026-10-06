# Deploying with Ansible

The `senhub.agent` collection installs the agent, writes its configuration
and checks that configuration before the agent sees it. One role,
`senhub.agent.agent`, does it on Debian, Ubuntu, RHEL and its derivatives,
SUSE, and Windows.

The collection lives in the agent's repository, under
`deploy/ansible/senhub/agent`, and is licensed Apache-2.0.

```bash
ansible-galaxy collection install senhub.agent      # once published on Galaxy
```

## A first playbook

```yaml
- name: SenHub Agent
  hosts: monitored
  become: true
  roles:
    - role: senhub.agent.agent
      vars:
        senhub_agent_edition: oss
        senhub_agent_channel: stable
        senhub_agent_global_tags:
          site: paris
          env: prod
        senhub_agent_probes:
          - name: web-db
            type: postgresql
            params:
              host: db.example.com
              username: monitor
              password: "${file:/etc/senhub-agent/pg_password}"
              interval: 60
        senhub_agent_strategies:
          otlp:
            endpoint: collector.example.com:4317
          http:
            port: 8080
            bind_address: 127.0.0.1
            endpoints: [prtg, web, nagios, prometheus]
```

Run it twice: the second run reports `changed=0`.

## What the role does on Linux

1. It adds the signed repository of the channel (`packages.senhub.io`). The
   key is downloaded and compared with the published fingerprint
   (`B998 7A2D 4623 796E 19D3 B185 CA56 F750 3545 30AF`) before anything
   trusts it; a key that does not match stops the run.
2. It installs `senhub-agent-oss` or `senhub-agent`. Changing the edition
   swaps the package and keeps the configuration, the agent key and the
   secrets.
3. It builds the configuration in a scratch copy of `/etc/senhub-agent`,
   runs `senhub-agent config check --json` on it, and copies the files over
   the live ones only if the check passes. A configuration the agent would
   refuse fails the play with the agent's own findings and leaves the host
   untouched.
4. It makes sure the service is enabled and running.

The agent watches `agent.yaml`, `probes.d/` and `strategies.d/` and loads
changes by itself, so a configuration change does not restart the service.
A new licence does: the agent reads it at start. Set
`senhub_agent_restart_on_config_change: true` to restart on every change
anyway.

## What the role does on Windows

The role reads the release manifest of the channel
(`https://packages.senhub.io/releases/<channel>/latest.json`, or
`releases/<version>/manifest.json` for a pinned version), takes the MSI of
the edition, checks that the manifest says `complete: true` (the Windows
files of a release are signed after the release is published; an
incomplete release is refused), downloads the MSI, compares its SHA-256 with
the manifest and only then runs it. The MSI properties of the
[silent install](installation.md#silent-unattended-install) are the
`senhub_agent_msi_*` variables. The configuration then follows the same
stage, check and copy sequence as on Linux.

!!! warning "Not yet run on a Windows host"

    The Windows tasks are written from the MSI and manifest documentation
    and have not been executed against a Windows machine. Try them on a test
    server before a fleet.

## Variables

| Variable | Default | Meaning |
|---|---|---|
| `senhub_agent_state` | `present` | `present` installs and configures, `absent` removes. |
| `senhub_agent_edition` | `oss` | `oss` (package `senhub-agent-oss`) or `full` (`senhub-agent`). |
| `senhub_agent_channel` | `stable` | `stable` or `beta`. |
| `senhub_agent_version` | empty | Empty installs if missing and never upgrades; `latest` follows the channel; `0.6.2` or `0.6.2-beta.1` pins (the packages spell the beta `0.6.2~beta.1`, both forms work). |
| `senhub_agent_license` | empty | Licence token (JWT), from an Ansible Vault variable. Written to `license.jwt`. |
| `senhub_agent_license_src` | empty | Path on the controller of a file holding the token, instead of `senhub_agent_license`. |
| `senhub_agent_probes` | `[]` | List of `{name, type, params}`, written to `probes.d/50-ansible.yaml`. |
| `senhub_agent_strategies` | `{}` | One key per output (`otlp`, `http`, `zabbix`, `prometheus`...); each becomes `strategies.d/50-<name>.yaml`. The key `http` replaces the packaged `00-http.yaml`. |
| `senhub_agent_global_tags` | `{}` | Merged into `agent.global_tags` of `agent.yaml`. |
| `senhub_agent_agent_settings` | `{}` | Other `agent.yaml` keys to merge; the generated agent key is kept. Rewrites `agent.yaml` without its comments. |
| `senhub_agent_remove_default_probes` | `false` | Delete the four probes the package ships (`probes.d/00-host.yaml`). |
| `senhub_agent_purge_stale` | `true` | Delete fragments the role wrote earlier and no longer declares (only files carrying its marker). |
| `senhub_agent_check_allow_warnings` | `false` | `config check` exits 1 on a warning; `true` applies the configuration anyway. |
| `senhub_agent_restart_on_config_change` | `false` | Restart the service on any configuration change. |
| `senhub_agent_manage_repository` | `true` | `false` for a mirror or a host that already has the repository. |
| `senhub_agent_repo_url` | `https://packages.senhub.io` | Base of the repositories. |
| `senhub_agent_repo_key_fingerprint` | the published one | Fingerprint the repository key must carry. |
| `senhub_agent_purge_data` | `false` | With `state: absent`, also delete the configuration, the state and the service user. |
| `senhub_agent_no_log` | `true` | Hide task output that may hold secrets. |
| `senhub_agent_msi_http_port`, `_tags`, `_otlp_endpoint`, `_zabbix_server`, `_zabbix_host_metadata`, `_desktop_shortcut`, `_install_folder`, `_adopt` | empty (`_desktop_shortcut`: `0`) | Windows: MSI properties `HTTP_PORT`, `TAGS`, `OTLP_ENDPOINT`, `ZABBIX_SERVER`, `ZABBIX_HOST_METADATA`, `DESKTOP_SHORTCUT`, `INSTALLFOLDER`, `ADOPT=1`; read at first install only. |
| `senhub_agent_manifest_base_url` | `https://packages.senhub.io/releases` | Windows: where the manifests are read. |
| `senhub_agent_allow_incomplete_release` | `false` | Windows: install a release whose manifest is not `complete`. Leave it off. |

The paths and service names (`senhub_agent_config_dir`, `senhub_agent_bin`,
`senhub_agent_service_name`, and the `senhub_agent_windows_*` ones) default
to what the packages and the MSI create.

## Outputs

A strategy is the content of a `strategies.d` file, one key per output:

```yaml
senhub_agent_strategies:
  otlp:
    endpoint: collector.example.com:4317
    headers:
      Authorization: "Bearer ${file:/etc/senhub-agent/otlp_token}"
  http:
    port: 8080
    bind_address: 0.0.0.0
    endpoints: [prtg, web, nagios, prometheus]
  zabbix:
    server: zabbix.example.com:10051
    host_metadata: senhub-agent
```

See [OTLP](otlp.md), [Prometheus](prometheus/index.md) and
[Zabbix](zabbix.md) for every key. Secrets belong in the
[secret store](secret-store.md) or behind `${file:...}` and `${env:...}`
references, not in the variables.

!!! tip "Probes from the environment"

    The agent also reads probes declared as `SENHUB_PROBE_<NAME>_TYPE`
    environment variables (see
    [Configuring probes from environment variables](configuration.md#configuring-probes-from-environment-variables)).
    The role does not use them: it writes fragments, which the check covers.

## Testing the role

`deploy/ansible/test.sh` converges the role on Debian 12, Ubuntu 24.04 and
Rocky Linux 9 containers with plain `ansible-playbook`, converges a second
time to require `changed=0`, then verifies that the service is active and the
live configuration passes `config check`. The same scenario is a Molecule
scenario in `extensions/molecule/default`.
