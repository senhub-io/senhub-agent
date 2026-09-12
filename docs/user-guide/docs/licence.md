# Licence

SenHub Agent is two things under one binary, and which licence applies
depends on which part you use.

## The open core

The binary, its collection engine, its output formats and most of its
probes are published under the [Apache License 2.0][apache], and their
source code is public at
[github.com/senhub-io/senhub-agent][repo]. You may use, modify and
redistribute them on the terms of that licence. Nothing on this page
restricts it, and nothing expires.

## The licensed probes

Seventeen probe types read equipment and software from third-party
vendors and are not covered by the Apache licence. Their use is granted
commercially by Sensor Factory, per customer and across the whole estate,
under the SenHub Agent licence agreement.

The agreement is drawn up in French, which is the only binding version.
An English translation is provided for convenience.

| Version | Standing | Document |
|---|---|---|
| French | Binding | [Contrat de licence SenHub Agent](licence/contrat-de-licence-senhub-agent.pdf) |
| English | Courtesy translation | [SenHub Agent Licence Agreement](licence/senhub-agent-licence-agreement.pdf) |

Where the two differ, for any reason, the French version prevails.

## What the agreement says about the control

Three points matter to anyone assessing the agent before deploying it,
and the agreement states them as undertakings rather than as intentions.

**The check is local.** The licence token is verified against a public
key inside the binary. The agent contacts no licence server, at
installation or at run time, and an agent cut off from every public
network behaves identically.

**Nothing is reported.** Sensor Factory receives no data about your
estate from the licence control: not the number of agents, nor their
identity, nor the probes you enable, nor the machines you monitor.

**Expiry degrades, it does not stop.** Seven days after a token expires,
the licensed probes stop collecting. The agent keeps running, the free
probes keep collecting, and the configured outputs keep sending.

## Which probes need a licence

The probe catalogue marks each type. In the web console, the Probes page
shows a lock on a type that needs one and names the reason; a type that
does not run on your operating system says so instead, so a lock always
means a licence and never a platform.

From the command line:

```bash
senhub-agent license status
```

[apache]: https://www.apache.org/licenses/LICENSE-2.0
[repo]: https://github.com/senhub-io/senhub-agent
