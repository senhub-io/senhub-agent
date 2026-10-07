# Deploying at scale

One host is installed by hand. A fleet is installed by the tool the estate
already runs, and the agent is built for that: the packages, the MSI and the
image are idempotent, their configuration is files or environment variables,
and the agent can check a configuration without applying it.

This section has one page per tool. Each page covers the same points, so the
pages can be compared: **pinning** the version, the **licence**, **secrets**,
**validation** before anything is applied, **upgrade** and **removal**, and
ends with the commands that **verify** the result.

## Which tool for which estate

| Estate | Tool | Page |
|---|---|---|
| Linux and Windows servers already managed by Ansible | The `senhub.agent` role | [Ansible](../ansible.md) |
| Windows machines joined to Active Directory | GPO software installation, with an MST | [Windows: Intune, GPO, SCCM](windows-intune-gpo.md) |
| Windows machines managed by Intune or Configuration Manager | The MSI as a Win32 app or an Application | [Windows: Intune, GPO, SCCM](windows-intune-gpo.md) |
| Cloud virtual machines created from an image or a template | `user-data` read at first boot | [cloud-init](cloud-init.md) |
| One or a few Docker hosts | A Compose file | [Docker Compose](docker-compose.md) |
| A Kubernetes cluster | The Helm chart, one agent per node | [Helm](helm.md) |
| Linux hosts that run containers under systemd, without Docker | A Podman Quadlet unit | [Podman](podman.md) |

When two rows fit, take the one your team already operates. The agent does
not care how it got there: a host installed by cloud-init can be taken over
by Ansible later, since both write the same files.

Three questions settle most choices:

- **Does the host keep its state between deployments?** A virtual machine
  does: its identity and its log bookmarks stay on its disk. A container
  does not, unless a volume is mounted on `/var/lib/senhub-agent`; see
  [the one mount that matters](../container.md#the-one-mount-that-matters).
- **Who owns the configuration afterwards?** With Ansible, Helm and a
  Compose file, the tool does: a change is made in its source and applied
  again. With cloud-init, the user-data runs once, so a later change is
  made by another tool or by hand.
- **Does the agent need to see the host?** On a virtual machine it does by
  default. On Kubernetes and Podman, watching the node or the host is an
  explicit choice, described in the Helm and Podman pages.

## What every tool relies on

The same facts hold on every page; they are stated once here.

| Fact | Where it is documented |
|---|---|
| The configuration is `agent.yaml`, `probes.d/*.yaml` and `strategies.d/*.yaml`. | [Configuration](../configuration.md) |
| `senhub-agent config check` validates a configuration and applies nothing. Exit `0` is clean, `1` warnings only, `2` an error; `--json` lists the findings. | [CLI reference](../cli.md#config-check) |
| A licence is a file, `license.jwt`, next to `agent.yaml`. The agent reads it at start. Without one, the free tier runs. | [Configuration](../configuration.md), [License](../license/index.md) |
| A secret is referenced, not written: `${file:/path}`, `${env:NAME}` or `${secret:NAME}`. | [Environment and file substitution](../configuration.md#environment-and-file-substitution), [Secret store](../secret-store.md) |
| A probe can be declared from the environment: `SENHUB_PROBE_<NAME>_TYPE`, `SENHUB_PROBE_<NAME>_<PARAM>`, and `SENHUB_PROBE_<NAME>_<PARAM>_FILE` to read a secret from a file. | [Probes from environment variables](../configuration.md#configuring-probes-from-environment-variables) |
| Failed log batches are kept on disk during a collector outage, in `otlp-queue/` of the state directory (up to 128 MiB and 24 hours by default). Give that directory a volume wherever the disk is not kept. | [Logs survive an outage](../otlp.md#logs-survive-an-outage) |
| Release tags carry no `v` prefix: `0.6.1`, not `v0.6.1`. | [Installation](../installation.md#direct-download-urls) |

## Two rules for a fleet

**One identity per agent.** Each agent has its own key, and the receiving
side tells agents apart by it. Never copy an `agent.yaml` that holds a key,
and never bake an identity into an image or a template. The packages, the
MSI and the image generate the key on first start; every page here leaves
that in place.

**Check, then apply.** A configuration the agent would refuse should never
reach a running host. The pages run the check where the tool allows one
before the file lands, and say plainly where it can only run after.

## Proving an example

Every example in this section is meant to be run as written, once the
placeholders (marked `<like this>`) are replaced. Each page ends with a
**Verify** block: the commands that show the install worked, in the order
to run them.
