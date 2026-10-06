# Downloading and verifying releases

A script, an Ansible role or a Dockerfile that installs the agent needs
four things: which version is current, the URL of the right file, its
checksum, and its signature. Each release publishes a **manifest** that
holds them, so none of this means reading the GitHub page.

## The addresses

Everything is served from `https://packages.senhub.io`, the site that
also hosts the [package repositories](installation.md#install-from-the-package-repositories).

| What | Address |
|---|---|
| Newest stable release | `https://packages.senhub.io/releases/stable/latest.json` |
| Newest beta | `https://packages.senhub.io/releases/beta/latest.json` |
| One release, forever | `https://packages.senhub.io/releases/<version>/manifest.json` |
| Every release, per channel | `https://packages.senhub.io/releases/index.json` |
| The schema | `https://packages.senhub.io/releases/manifest.schema.json` |

`<version>` is the release tag, without a `v` (`0.6.2`, `0.6.2-beta.1`).

`latest.json` is a copy of the manifest of the highest version of its
channel. A beta never appears in `stable`, and `beta` follows betas only:
a machine that wants "the newest beta, or the final release once it is
out" reads both and picks the higher `version`.

!!! note "Why not GitHub's `/releases/latest/download/`"

    That address ignores pre-releases, knows nothing of channels, and can
    only redirect to one file name, so it cannot tell you a checksum or a
    signature, nor that the Windows files are still waiting to be signed.
    The manifest carries all of that and is the same for a script and for a
    person.

## What a manifest holds

```json
{
  "schema_version": 1,
  "version": "0.6.2-beta.1",
  "channel": "beta",
  "date": "2026-10-06T08:05:15Z",
  "commit": "aac44564b4536f978a79d63d4492ebe52a53c408",
  "complete": false,
  "signing_pending": ["senhub-agent-windows-amd64.zip", "senhub-agent-oss-windows-amd64.zip"],
  "artifacts": [
    {
      "name": "senhub-agent-linux-amd64.zip",
      "os": "linux", "arch": "amd64", "edition": "full", "kind": "zip",
      "url": "https://github.com/senhub-io/senhub-agent/releases/download/0.6.2-beta.1/senhub-agent-linux-amd64.zip",
      "sha256": "f8ecc56d046e4e6587b1c6422c64ff2ff2286c1235d069919c91e24ddebca957",
      "size": 28994471,
      "minisig_url": "https://github.com/senhub-io/senhub-agent/releases/download/0.6.2-beta.1/senhub-agent-linux-amd64.zip.minisig"
    }
  ],
  "images": [
    {
      "repo": "ghcr.io/senhub-io/senhub-agent",
      "edition": "full",
      "tag": "0.6.2",
      "digest": "sha256:..."
    }
  ]
}
```

| Field | Meaning |
|---|---|
| `version`, `channel` | The release tag, and `stable` or `beta`. |
| `date` | When the release was published, UTC. |
| `commit` | The commit of the open source repository the release is tagged at. |
| `complete` | `true` when every ZIP and MSI carries its signature and, on a stable release, both MSIs are there. |
| `signing_pending` | The ZIPs and MSIs that have no signature yet. |
| `artifacts[].kind` | `zip` (the archive the auto-updater reads), `msi`, `deb`, `rpm`. |
| `artifacts[].edition` | `full` or `oss`. |
| `artifacts[].os`, `arch` | `linux` or `windows`; `amd64` or `arm64` (an RPM built for `x86_64` or `aarch64` is listed as `amd64` or `arm64`). |
| `artifacts[].url` | The GitHub release asset. |
| `artifacts[].sha256`, `size` | Checksum and size in bytes, as GitHub computed them at upload. |
| `artifacts[].minisig_url` | The detached signature, or `null`. |
| `images[]` | The container images of this exact version, with the digest to pull by. |
| `charts[]` | The Helm chart of this exact version (`repo`, `version`, `digest`), published as an OCI artifact; empty when it is not published yet. |

Three things worth knowing:

- **A Windows file has no signature until the signing round has run.**
  The manifest published with a release reads `"complete": false`; it is
  rebuilt when the Windows ZIPs are signed and again when the MSIs are.
  Do not install a Windows file from a manifest that is not `complete`.
- **`.deb` and `.rpm` have `"minisig_url": null` by design.** They are
  signed with the repository key (`https://packages.senhub.io/gpg.key`); install them from the
  [package repositories](installation.md#install-from-the-package-repositories),
  where the package manager does the verification.
- **There is no `min_upgrade_from`.** The agent has no rule that refuses an
  upgrade from an older version, so the manifest does not pretend to know
  of a minimum. If one appears, it will be a new field of the same schema
  version.

The schema may gain fields without a version change: ignore the ones you
do not know. A rename, a removal or a change of meaning raises
`schema_version`.

The images and charts lists are filled by the release itself: the release
publishes both images and the Helm chart, waits for them, then builds the
manifest.

## What is published, and when

Cutting a release publishes everything, in this order, with no step to
remember: the ZIPs and their signatures, the `.deb` and `.rpm` packages and
repositories, the container images of both editions (and the minor tag of
a stable release), the Helm chart, then the manifest. The only manual step left is the
Authenticode signing of the Windows files; the manifest is rebuilt after
the Windows ZIPs are signed and again after the MSIs, and says
`"complete": false` until then.

If a step fails (a package build, an image scan), the run fails and names
it, and the manifest still lists what exists. Running the "Publish
release manifest" workflow again for the tag completes it.

## Finding and verifying the right file

The public key below is the one the agent embeds to verify its own
updates. It is the same for every release.

```bash
CHANNEL=stable          # or: beta
EDITION=oss             # or: full
OS=linux ARCH=amd64
KEY=RWRlfkyeLpjI0MjTSfuvT/bDNHHaVJhRirQN8Z8LTAM+n4LKVbpjrlRh

manifest=$(curl -fsSL "https://packages.senhub.io/releases/$CHANNEL/latest.json")

# Refuse a release whose signing round is not finished.
[ "$(jq -r .complete <<<"$manifest")" = true ] || { echo "release not complete yet" >&2; exit 1; }

file=$(jq -c --arg e "$EDITION" --arg o "$OS" --arg a "$ARCH" \
  '.artifacts[] | select(.kind == "zip" and .edition == $e and .os == $o and .arch == $a)' <<<"$manifest")

name=$(jq -r .name <<<"$file")
curl -fsSLo "$name"          "$(jq -r .url <<<"$file")"
curl -fsSLo "$name.minisig"  "$(jq -r .minisig_url <<<"$file")"

echo "$(jq -r .sha256 <<<"$file")  $name" | sha256sum -c -     # shasum -a 256 -c - on macOS
minisign -Vm "$name" -P "$KEY"
```

The checksum catches a corrupted download; the signature proves the file
was built and signed by SenHub. Do both: the manifest and the file come
from different hosts, and the signature is what ties them.

To stay on one minor line, take the newest of it from the index:

```bash
curl -fsSL https://packages.senhub.io/releases/index.json \
  | jq -r '.channels.stable.releases[] | select(.version | startswith("0.6.")) | .version' | head -n 1
```

(the list is newest first).

To pin a release in an Ansible role or a Dockerfile, read
`https://packages.senhub.io/releases/<version>/manifest.json` instead of
`latest.json`: it never changes except to gain a signature or an image.

## Container image tags

Images are published to `ghcr.io/senhub-io/senhub-agent` (full edition)
and `ghcr.io/senhub-io/senhub-agent-oss` (open source edition).

| Tag | Moves? | Use it for |
|---|---|---|
| `0.6.2` | Never | A reproducible deployment. |
| `0.6` | To each new patch of that minor line, stable releases only | Following the fixes of one minor line. |
| `0.6.2-beta.1` | Never | Testing a beta. A beta never moves `0.6`. |

There is no `latest` tag: it would cross minor lines without notice.
For a deployment that must be immune even to a moved tag, pull by the
digest that the manifest lists under `images`.

## Helm chart

The chart is published to `oci://ghcr.io/senhub-io/charts/senhub-agent`
by the release, with the release version as its version (`0.6.2`,
`0.6.2-beta.1`). A beta is only installed with an explicit `--version`.
See [Kubernetes (Helm)](kubernetes-helm.md).
