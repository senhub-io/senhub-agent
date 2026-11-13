# Next (unreleased)

Nothing released yet since 0.7.1. Changes land here as they are merged.

- **`config check` reports keys of the `agent:` block that do nothing.** A key the agent does not read inside `agent:` was dropped without a word: a misspelt `licence:`, or settings of older versions such as `update_check_interval`, `mode` or `generated`. `config check` now names each one in a warning (exit code 1), as it already did for top-level and probe keys. Remove them from `agent.yaml`; nothing else changes, the update check still runs every hour, at start and on every configuration change.

<div class="rn-filter"></div>
