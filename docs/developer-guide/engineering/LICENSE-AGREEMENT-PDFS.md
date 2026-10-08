# License agreement PDFs

The two printable PDFs under `docs/user-guide/docs/license/` are committed
binaries. They are rendered from the agreement markdown held in the private
`adv-commerce` repository (`documents/contrat-licence-agent/`) by its
`render.py`, which needs macOS Chrome and cannot run in CI. They carry the
version of the agreement (currently 1.0), not the agent version.

Each PDF has a sidecar, `<pdf name>.source.sha256`, with three lines:

```
agreement_version 1.0
source <markdown file name> <sha256 of the markdown it was rendered from>
pdf <sha256 of the PDF>
```

## What the guard proves

`TestLicensePDFsHaveMatchingSidecars` (`internal/docscoverage`, run by
`make test`) fails when a PDF has no sidecar, when a sidecar has no PDF, or
when the recorded `pdf` hash differs from the committed file.

CI cannot see `adv-commerce`, so it cannot re-render or re-hash the source.
The guard proves that PDF and sidecar travel together, and that the PDF is
the one `render.py` produced from the recorded source and version. It does
not prove that the source is still the current markdown.

## Regenerating

1. In `adv-commerce`, run `render.py` on the agreement markdown (see its
   docstring for the arguments). It writes the PDF and its sidecar side by
   side.
2. Copy both files here, keeping the committed PDF name (the French PDF is
   `contrat-de-licence-senhub-agent.pdf`, so its sidecar is
   `contrat-de-licence-senhub-agent.pdf.source.sha256`).
3. Run `make test` and commit the PDF and sidecar in the same commit.

The third-party annex inside the PDF is built from `THIRD-PARTY-NOTICES.md`
and is not covered by the source hash.
