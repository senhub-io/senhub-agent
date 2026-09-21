# License

SenHub Agent is two things under one binary, and which license applies
depends on which part you use.

## The open core

The binary, its collection engine, its output formats and most of its
probes are published under the [Apache License 2.0][apache], and their
source code is public at
[github.com/senhub-io/senhub-agent][repo]. You may use, modify and
redistribute them on the terms of that license. Nothing on this page
restricts it, and nothing expires.

## The licensed probes

Some probe types read equipment and software from third-party vendors and
are not covered by the Apache license. Their use is granted commercially
by Sensor Factory, per customer and across the whole estate, under the
SenHub Agent license agreement.

The agreement is drawn up in French, which is the only binding version.
An English translation is provided for convenience. Both are published as
web pages; a printable PDF of each is linked alongside.

| Version | Standing | Read | Download |
|---|---|---|---|
| French | **Binding** | [Contrat de licence SenHub Agent](agreement-fr.md) | [PDF](contrat-de-licence-senhub-agent.pdf) |
| English | Courtesy translation | [SenHub Agent License Agreement](agreement-en.md) | [PDF](senhub-agent-license-agreement.pdf) |

Where the two differ, for any reason, the French version prevails.

## What the agreement says about the control

Three points matter to anyone assessing the agent before deploying it,
and the agreement states them as undertakings rather than as intentions.

**The check is local.** The license token is verified against a public
key inside the binary. The agent contacts no license server, at
installation or at run time, and an agent cut off from every public
network behaves identically.

**Nothing is reported.** Sensor Factory receives no data about your
estate from the license control: not the number of agents, nor their
identity, nor the probes you enable, nor the machines you monitor.

**Expiry degrades, it does not stop.** The licensed probes keep working
for at least seven days after a token expires, then stop collecting. The agent keeps running, the free
probes keep collecting, and the configured outputs keep sending.

## Third-party components

The binary links components published by third parties under their own
licences, which apply to those components alone and prevail, for them, over
the agreement. The list is generated from the build's dependency graph and
is published as [Third-party components](third-party.md), which is Annex 1
of the agreement.

## Which probes need a license

The probe catalogue marks each type. In the web console, the Probes page
shows a lock on a type that needs one and names the reason; a type that
does not run on your operating system says so instead, so a lock always
means a license and never a platform.

From the command line:

```bash
senhub-agent license show
```

[apache]: https://www.apache.org/licenses/LICENSE-2.0
[repo]: https://github.com/senhub-io/senhub-agent
