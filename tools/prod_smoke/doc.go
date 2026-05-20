// Package prod_smoke contains assertions that run against the live
// production agents on sha901 (Linux) and bbcloud (Windows). They
// are NOT part of the default `make test` run — gated by the
// `prod_smoke` build tag so the cost (real SSH connections, log
// pulls, prod traffic) only fires when an operator explicitly asks.
//
// # Running
//
//   make prod-smoke              # all targets
//   go test -tags=prod_smoke -v -run TestBearerLeak ./tests/prod_smoke
//
// # Required env vars (or skip)
//
// The package shells out to `~/.senhub/read-secret.sh` to resolve
// SSH credentials. Tests that target a host SKIP (not fail) when the
// corresponding secret is unavailable — `make prod-smoke` thus stays
// usable from a workstation that doesn't have prod access for one of
// the hosts.
//
// # Conventions
//
//   - Each test pins an invariant the production agent must satisfy
//     after the Round 1 security fixes land. The test names trace
//     1:1 to the PRs:
//       TestBearerLeak_NoBearerInLog   <- PR #120
//       TestProbeParamsLeak_NoUserInLog <- PR #121
//       TestNoDowngrade_StaleConfigIgnored <- PR #122
//   - Helpers in sshutil.go shell out via sshpass + ssh, never embed
//     credentials in the command line (env-passing only).
//   - Tests assume the agent is already deployed at the version
//     specified by SENHUB_EXPECTED_AGENT_VERSION (default
//     "0.1.95-beta"). They report context, never destructively
//     mutate the host except where the test name says so.
package prod_smoke
