# Self-update artifact signing

Since #266, the agent refuses to apply a self-update whose artifact is
not signed with the release minisign key. Integrity of the update
channel no longer rests on TLS to the (config-settable) registry URL.

## How verification works

1. The agent downloads the per-platform release archive
   (`senhub-agent-<os>-<arch>.zip`) from the registry.
2. It downloads the detached signature published next to it:
   `senhub-agent-<os>-<arch>.zip.minisig`.
3. It verifies the signature over the raw archive bytes — before the
   archive is even parsed — against a minisign public key **embedded in
   the binary at build time**.
4. Only then is the inner binary extracted and applied atomically.

Any failure (missing signature, bad signature, key absent from the
build) rejects the update, logs an error, and increments the
`senhub.agent.update.rejected{reason}` self-metric
(`signature_unavailable` | `signature_invalid`). Alert on this counter
rising: it is either a mis-published release or an attempted
supply-chain tamper.

## Fail-closed semantics

- The public key is injected at build time:
  `make build UPDATE_SIGNING_PUBKEY="RWQ..."` (an `-X` ldflag on
  `internal/agent/services/auto_update.signingPublicKey`).
- A build without an embedded key **refuses to self-update** ("this
  build embeds no update signing key"). Manual installs keep working —
  only the auto-update path is gated.
- The key is deliberately **not configurable at runtime**: a
  config-settable key would defeat the protection, since the registry
  URL is itself config-settable.

## Release pipeline requirements

For auto-update to function on a release, the pipeline must:

1. Sign each platform archive with the release private key:
   `minisign -Sm senhub-agent-<os>-<arch>.zip` (produces `.minisig`).
2. Publish every `.minisig` next to its archive on the registry
   (`/download/<version>/…`) and in the GitHub release assets.
3. Build the binaries with `UPDATE_SIGNING_PUBKEY` set to the matching
   public key.

Key custody (where the private key lives, who can sign, rotation) is an
organizational decision documented alongside the release process — the
private key never appears in this repository or its CI configuration.
