# Security policy

## Reporting a vulnerability

Send reports to **security@sensorfactory.fr**. Please do not open a public
issue, pull request or discussion for a suspected vulnerability.

A useful report says:

- the agent version (`senhub-agent version`) and the operating system;
- the component concerned (a probe, an output, the console, the updater,
  the packages or the installer);
- how to reproduce the problem, and what an attacker gains;
- whether the problem is already known to others or public.

## What happens next

| Step | Commitment |
|---|---|
| Acknowledgement of your report | within 5 business days |
| First assessment (confirmed or not, severity) | within 10 business days |
| Progress update until the fix ships | at least every 30 days |
| Coordinated disclosure | 90 days after the report, or earlier once a fix is released |

We agree a disclosure date with you. If a fix needs more than 90 days, we
tell you why and propose a new date.

## Supported versions

Only the latest stable release receives security fixes. Beta releases and
older stable releases do not: the fix ships in a new stable release, and
users of an older one upgrade to it.

The current stable release is the one listed first on the
[releases page](https://github.com/senhub-io/senhub-agent/releases)
without the pre-release flag.

## Scope

This policy covers the SenHub agent: the code in this repository, the
released binaries, the MSI installer, the `.deb` and `.rpm` packages
published on packages.senhub.io, and the container image and Helm chart
built from this repository.

The contact point above is the one Sensor Factory designates for
vulnerability reports on its products with digital elements.
