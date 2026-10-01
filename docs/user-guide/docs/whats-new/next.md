# Next (unreleased)

Changes land here as they are merged to `dev`.

<div class="rn-filter"></div>

## Breaking Changes

- **Exit codes of the command line are a contract.** `0` done, `1`
  warning, `2` failure, `3` unchanged. Failures that exited `1` now exit
  `2`, and `config check` exits `1` when it found warnings only (a missing
  licence is one) and `status` exits `1` when the service is stopped, the
  agent does not answer or it reports itself unhealthy. A script that
  tests for any non-zero code is unaffected; one that compares with `1`
  must follow. The container image and the Windows installer are updated
  to match. See [Exit codes](../cli.md#exit-codes).

## Features

- **`--json` output.** `version`, `status`, `config check`, `config
  show`, `config set`, `config init` and `secret status` print one JSON
  object with a `senhub.cli.<command>/v1` schema identifier. Failures
  are JSON objects too. See [JSON output](../cli.md#json-output).
- **Idempotent provisioning commands.** `config init`, `config set` and
  `install` run again on a machine already in the requested state write
  nothing and exit `3`. `config init --ok-if-unchanged` exits `0` in that
  case, for installers that treat any other code as a failure.
