# Next (unreleased)

Nothing released yet since 0.6.3. Changes land here as they are merged.

<div class="rn-filter"></div>

## Fixes

- **Redis 8 key-size lines are no longer read as databases.** Channels such as `DB 0_distrib_strings_sizes Keys` disappear; only `dbN` lines of the keyspace make a database.
- **An empty Redis keeps its db0 channels.** Redis prints no keyspace line for a database without keys; the probe now reports `DB 0 Keys`, `DB 0 Expiring Keys` and `DB 0 Average TTL` at 0, so an alert on the key count keeps working when the instance is emptied. A Sentinel, which has no keyspace, gets none.
- **The Redis replication role reads "Sentinel or unknown" at -1.** The PRTG lookup said "Unknown" with a warning on every Sentinel; the Redis page now gives the three values and how to install the lookups.
