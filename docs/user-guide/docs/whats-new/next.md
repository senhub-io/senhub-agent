# Next (unreleased)

Changes since 0.6.0, collected as they are merged.

<div class="rn-filter"></div>

## Fixes

- **The console's Agent card, `senhub-agent status` and `/health` report
  measured values.** CPU was a constant 0 %, and "Memory" was the Go
  heap, about a tenth of what the operating system charges to the agent.
  Memory is now the resident set (the working set on Windows), with the
  heap alongside, and CPU the agent's share of the whole machine over the
  last interval. Uptime counts from the process start. `/health` returned
  `"version": "HTTP Strategy v1.0"` and `info/system` a nested version of
  `"unknown"`; both carry the release now. `info/system` reported
  `cache.probe_count: 0` and a `cache.memory_usage` that was the process
  heap: the first is the real count, the second is gone.

- **The console counts Pro probe types the same way on every page.** The
  catalogue said "18 Pro types unlock with a license" where the Overview
  and Settings said 17: it counted a Pro type that does not run on this
  operating system, which no licence unlocks. Such a type is now counted
  with the types that run on another platform only. After a failed test,
  the probe editor printed "0 metrics" whatever the test had collected;
  it gives the real count.

- **Previewing a sensor URL no longer counts as a poller.** The console's
  preview reads the same route as PRTG or Nagios, so opening the Sensor
  URLs tab turned "no PRTG request seen" into "last PRTG request just
  now" for a sensor that did not exist yet. The export counts on the
  Outputs page now say they run since the agent started.

- **Test connection tests every push output.** On a Zabbix or SenHub
  cloud output it answered "test failed" in red, the same as an
  unreachable server, because no test existed. Zabbix now opens a TCP
  connection to each server or proxy address, SenHub cloud reaches the
  intake the agent pushes to. For the events output, the test reached
  the base URL; it now reaches `/event/insert`, where the agent posts.

- **Two development endpoints are gone.** `POST
  /api/{key}/debug/inject-test-metrics` and `inject-real-metrics`,
  reachable with the administration key, wrote invented Dell PowerVault
  series into the cache that PRTG, Nagios and Prometheus read, and
  answered with links to a developer's agent on `localhost:8080`.

- **The console's API reference lists the routes this agent serves.** Its
  endpoint list and count came from a hand-written table of 16 routes
  that missed most of the configuration, catalogue and information
  routes; they are now read from the agent's router. The page also
  rewrote `/admin/` paths to `/debug/`, showing routes that do not exist.
