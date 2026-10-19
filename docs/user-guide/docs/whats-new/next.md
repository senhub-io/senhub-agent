# Next (unreleased)

Changes land here as they are merged.

<div class="rn-filter"></div>

## Before you upgrade

- **PRTG sensors no longer carry the Check Result channel by default.** 0.6.4 added it to every database, application and active-check probe, referencing the `senhub.probe.check.result` lookup; on a PRTG core without the SenHub lookup files the sensor turned Warning (PE272). The channel is now off unless `prtg.check_result_channel: true` is set in the `http` output, and then it keeps the same lookup name and values. PRTG keeps a channel that stops receiving data: if you installed the lookups and want to keep the channel, set the option before upgrading (0.6.4 runs with it and ignores it; only its `config check` reports it as unread); otherwise delete the channel or re-add the sensor. See [Installing PRTG value lookups](../web-interface.md#installing-prtg-value-lookups).
