# Next (unreleased)

Nothing released yet since 0.6.0. Changes land here as they are merged to `dev`.

<div class="rn-filter"></div>

## Features

- **The Zabbix server is set at install time.** The MSI takes
  `ZABBIX_SERVER` (and `ZABBIX_HOST_METADATA`), `config init` takes
  `--zabbix-server`, the container `SENHUB_ZABBIX_SERVER`. With a server
  prepared by `zabbix setup`, installing the agent is the only step: the
  host registers and fills in by itself, as with the Zabbix agent's
  installer. Until now the output had to be written by hand after the
  install.
