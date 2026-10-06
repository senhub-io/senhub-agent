# Molecule scenarios

`default` converges the role on Debian 12, Ubuntu 24.04 and Rocky Linux 9
(systemd containers from `geerlingguy/docker-*-ansible`), converges it a
second time to prove it changes nothing, then runs `verify.yml`.

```bash
cd deploy/ansible/senhub/agent
molecule test
```

The scenario installs from the real `packages.senhub.io` repository. The
channel and edition come from `SENHUB_TEST_CHANNEL` (default `beta`) and
`SENHUB_TEST_EDITION` (default `oss`, which needs no licence).

Without Molecule, `deploy/ansible/test.sh` runs the same `converge.yml` and
`verify.yml` against the same images with plain `ansible-playbook`.

## Windows

There is no Windows scenario: the Molecule docker driver cannot run a
Windows guest. The Windows tasks (`tasks/windows*.yml`, `configure_windows.yml`)
have to be exercised against a Windows Server VM, for example with the
`delegated` driver and an inventory of one Windows host reachable over
WinRM or SSH:

```yaml
driver:
  name: default
  options:
    managed: false
    login_cmd_template: ""
platforms:
  - name: win2022
```

with the host's connection variables (`ansible_connection: winrm` or `psrp`)
in `group_vars`. The same `converge.yml` applies, with
`senhub_agent_msi_*` properties added, and a `verify.yml` that queries the
`senhub-agent` service and runs `senhub-agent.exe config check --json`.
These tasks were written from the MSI and release-manifest documentation and
have not been run.
