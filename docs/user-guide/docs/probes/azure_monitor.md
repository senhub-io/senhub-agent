<img src="../../assets/probe-logos/azure_monitor.svg" alt="" class="probe-page-logo probe-page-logo-si">

!!! warning
    **License: Pro** - Requires a Pro or Enterprise license.

# Overview

The Azure Monitor probe finds the Azure resources of one or more subscriptions and publishes their platform metrics, as Azure Monitor holds them, without a list to maintain. Resources are discovered through **Azure Resource Graph**, filtered by ARM type, resource group and tag; the metrics of each resource are then read through the Azure Monitor metrics API of Azure Resource Manager. Authentication is an Entra ID app registration (client credentials): nothing is installed on the monitored resources.

One probe instance covers one app registration and one or more subscriptions. A resource created after the agent started is picked up at the next discovery; one that is deleted, or that leaves the scope through a filter, stops being read.

**Collected data:**

- Every platform metric of every discovered resource, with its primary aggregation, as a gauge
- A small curated set of conventional names (CPU and memory utilization, for example) emitted in addition
- Two state series per resource: provisioning state and product state (running, stopped, ...)
- Optional figures the platform metrics do not carry: Service Bus queue, topic and subscription message counts, App Service Plan usages, free disk space of virtual machines from Log Analytics
- The number of resources a discovery filter left out
- Optionally, one `cloud.resource` entity per discovered resource (see [Entities](#entities))

# Quick Start

```yaml
# probes.d/41-azure-monitor.yaml — each file under probes.d/ is a YAML array of probes
- name: azmon
  type: azure_monitor
  params:
    tenant_id: "00000000-0000-0000-0000-000000000000"
    client_id: "11111111-1111-1111-1111-111111111111"
    client_secret: "${secret:azmon.client_secret}"   # OS secret store; inline plaintext is auto-sealed on install
    subscription_ids:
      - "22222222-2222-2222-2222-222222222222"
    discovery:
      include_types:
        - Microsoft.Compute/virtualMachines
        - Microsoft.Web/sites
        - Microsoft.ServiceBus/namespaces
      exclude_resource_groups: [rg-sandbox]
      tags:
        env: prod
    tags_allow: [env, owner]
```

Without a `discovery` block the probe follows every resource of the subscription it can read, up to `max_resources` (500).

# Requirements

- An **Entra ID app registration** with a **client secret**.
- Azure roles, assigned to the app registration on each subscription (or on the resource groups to cover):

| Role | Why | Where |
|---|---|---|
| `Reader` | Discovery through Resource Graph, and the Service Bus and App Service Plan figures | The subscription, or the resource groups to cover |
| `Monitoring Reader` | Reading the platform metrics | The same scope |
| `Log Analytics Reader` | Only for the VM disk figure (`plugins.vm_disk_log_analytics`) | The Log Analytics workspace |

- Outbound HTTPS from the agent host to `login.microsoftonline.com`, `management.azure.com` and, for the VM disk figure, `api.loganalytics.io`.

Azure returns, from a subscription-wide listing, only the resources the identity is allowed to read. Granting the roles on one resource group therefore bounds the probe to that group, with no list to write.

## Shared credentials

To avoid repeating the identity of the app registration in every entry, set it once in the agent's environment. It serves every Azure probe (`azure_monitor`, `azure_log_query`, `entra_id`, `azure_container_apps`, `azure_container_app_jobs`). A probe that sets a key itself keeps its own value for that key.

| Variable | Replaces |
|---|---|
| `SENHUB_AZURE_TENANT_ID` | `tenant_id` |
| `SENHUB_AZURE_CLIENT_ID` | `client_id` |
| `SENHUB_AZURE_CLIENT_SECRET` | `client_secret` |
| `SENHUB_AZURE_CLIENT_SECRET_FILE` | path of a file holding the secret (a mounted secret, for example) |
| `SENHUB_AZURE_SUBSCRIPTION_ID` | `subscription_ids` (a single subscription) |

The secret is taken from the first of these that is set: `client_secret`, `client_secret_file`, `SENHUB_AZURE_CLIENT_SECRET`, `SENHUB_AZURE_CLIENT_SECRET_FILE`. A secret file that cannot be read is an error, not a fallback. The variables are read when the probe is configured, so changing them needs a restart.

# Configuration Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->
<!-- sha256:cd2fb5ae7ac7aa6c7aa537329b876fe5561f72c35a07218b902525a586af7a02 -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `tenant_id` | In practice | - | Entra ID tenant (directory) ID; required unless SENHUB_AZURE_TENANT_ID is set for every Azure probe of the agent |
| `client_id` | In practice | - | Application (client) ID of the app registration; required unless SENHUB_AZURE_CLIENT_ID is set |
| `client_secret` | In practice | - | Client secret of the app registration; one of client_secret, client_secret_file, SENHUB_AZURE_CLIENT_SECRET or SENHUB_AZURE_CLIENT_SECRET_FILE is required, in that order. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `client_secret_file` | No | - | File holding the client secret; read when the probe is configured. Used when client_secret is not set |
| `subscription_ids` | In practice | - | Subscriptions to discover and read; the application needs Reader (discovery) and Monitoring Reader (metrics) on each. SENHUB_AZURE_SUBSCRIPTION_ID is used when empty |
| `subscription_id` | No | - | A single subscription, when subscription_ids is not set |
| `interval` | No | `60` | Seconds between two metric reads |
| `discovery` | No | - | What Resource Graph is asked for; ignored when 'resources' is set |
| `discovery.interval` | No | `900` | Seconds between two discoveries |
| `discovery.include_types` | No | - | Follow only these ARM types (Microsoft.Compute/virtualMachines); case is ignored |
| `discovery.exclude_types` | No | - | Never follow these ARM types |
| `discovery.include_resource_groups` | No | - | Follow only these resource groups |
| `discovery.exclude_resource_groups` | No | - | Never follow these resource groups |
| `discovery.tags` | No | - | Follow only resources carrying these tags; an empty value requires the tag to exist |
| `discovery.max_resources` | No | `500` | Hard cap on followed resources; what is left out is named in the log |
| `resources` | No | - | Explicit ARM resource ids; when set, they replace discovery |
| `metrics` | No | - | Per ARM type, restrict what is read to an allow-list of Azure metric names, optionally split by one dimension |
| `metrics[].type` | No | - | ARM type, for example Microsoft.Storage/storageAccounts |
| `metrics[].names` | No | - | Azure metric names to read; empty reads every metric without a required dimension |
| `metrics[].dimension` | No | - | Dimension to split the listed metrics by |
| `metrics[].dimension_values` | No | - | Values of the dimension to read; empty reads every value |
| `tags_allow` | No | - | Azure tags published as azure.resource.tag.<name>; none by default, to keep cardinality bounded |
| `plugins` | No | - | Figures the platform metrics do not carry |
| `plugins.servicebus` | No | `true` | Queue, topic and subscription message counts of discovered Service Bus namespaces |
| `plugins.app_service_plan` | No | `true` | Usages of discovered App Service Plans |
| `plugins.vm_disk_log_analytics` | No | `false` | Free disk space of virtual machines from Log Analytics (needs VM Insights) |
| `plugins.workspace_id` | No | - | Log Analytics workspace ID (GUID) for vm_disk_log_analytics |
| `entities` | No | - | Entity emission for Toise |
| `entities.enabled` | No | `false` | Emit a cloud.resource entity per discovered resource; turn on only once the tenant's Toise carries the type |
| `authority_host` | No | `login.microsoftonline.com` | Entra ID authority, for sovereign clouds |
| `management_host` | No | `management.azure.com` | Azure Resource Manager endpoint, for sovereign clouds |
| `log_analytics_host` | No | `api.loganalytics.io` | Log Analytics query endpoint, for sovereign clouds |

<!-- schema:params:end -->

`resources` entries must be full ARM ids (`/subscriptions/.../resourceGroups/.../providers/...`); anything else is refused when the probe is configured.

## Choosing metrics

By default every metric of every resource is read, except those that require a dimension. To narrow one type, or to split chosen metrics by a dimension:

```yaml
    metrics:
      - type: Microsoft.Storage/storageAccounts
        names: [Transactions]
        dimension: ResponseType
        dimension_values: [Success, ServerTimeoutError]
```

The dimension value is published as the attribute `azure.monitor.dimension.<dimension in snake_case>`.

# Metrics Collected

## Tier 1: every platform metric

Each platform metric of a discovered resource is published, with its primary aggregation, as a **gauge** (a per-grain total is not a counter).

- **Name**: `azure.<product>.<metric name in snake_case>`. `<product>` comes from the table below; a type that is not in the table gets `azure.<provider>.<type>`, the provider without its `Microsoft.` prefix and each segment in snake case and singular (`Microsoft.Foo/barThings` becomes `azure.foo.bar_thing`).
- **Value**: unchanged.
- **Unit**: translated to UCUM. Percent is `%`, Bytes `By`, BytesPerSecond `By/s`, BitsPerSecond `bit/s`, Seconds `s`, MilliSeconds `ms`, Count `{count}` (or a precise annotation such as `{message}` or `{request}` for well-known counts), CountPerSecond `{count}/s`, Cores `{core}`, Unspecified `1`. A unit Azure reports that is not in this list is published as `1`.
- **Attributes**: `azure.monitor.metric.name` (Azure's raw name), `azure.monitor.aggregation`, `azure.monitor.time_grain`, plus `azure.monitor.dimension.<name>` where a dimension is read and `azure.monitor.sub_resource` where the metric belongs to a sub-resource.

Example: the `Percentage CPU` metric of a virtual machine is `azure.vm.percentage_cpu`, unit `%`.

| ARM type | Product |
|---|---|
| `Microsoft.Compute/virtualMachines` | `azure.vm` |
| `Microsoft.Compute/virtualMachineScaleSets` | `azure.vm.scale_set` |
| `Microsoft.Compute/disks` | `azure.disk` |
| `Microsoft.Web/sites` | `azure.app_service` (`azure.functions` for a Function App) |
| `Microsoft.Web/serverFarms` | `azure.app_service.plan` |
| `Microsoft.Web/hostingEnvironments` | `azure.app_service.environment` |
| `Microsoft.ContainerService/managedClusters` | `azure.aks` |
| `Microsoft.App/containerApps` | `azure.container_apps` |
| `Microsoft.ContainerInstance/containerGroups` | `azure.container_instances` |
| `Microsoft.DocumentDB/databaseAccounts` | `azure.cosmosdb` |
| `Microsoft.ServiceBus/namespaces` | `azure.servicebus` |
| `Microsoft.EventHub/namespaces` | `azure.eventhubs` |
| `Microsoft.Storage/storageAccounts` | `azure.storage` |
| `Microsoft.Sql/servers/databases` | `azure.sql.database` |
| `Microsoft.Sql/servers` | `azure.sql.server` |
| `Microsoft.KeyVault/vaults` | `azure.keyvault` |
| `Microsoft.Network/loadBalancers` | `azure.load_balancer` |
| `Microsoft.Network/applicationGateways` | `azure.application_gateway` |
| `Microsoft.Network/publicIPAddresses` | `azure.public_ip` |
| `Microsoft.Cache/redis` | `azure.redis` |
| `Microsoft.DBforMySQL/flexibleServers` | `azure.mysql.flexible_server` |
| `Microsoft.DBforPostgreSQL/flexibleServers` | `azure.postgresql.flexible_server` |
| `Microsoft.EventGrid/topics` | `azure.eventgrid.topic` |
| `Microsoft.ApiManagement/service` | `azure.api_management` |
| `Microsoft.OperationalInsights/workspaces` | `azure.log_analytics.workspace` |
| `Microsoft.Insights/components` | `azure.application_insights` |

## Tier 2: curated names

For a few conventional measures the probe also emits a stable name, scaled to a 0-1 ratio where Azure reports a percentage. They are meant for alert rules; the tier 1 series is still published next to them.

| Metric | Unit | Source (ARM type, Azure metric) |
|---|---|---|
| `azure.vm.cpu.utilization` | `1` | Virtual machine, `Percentage CPU` (divided by 100) |
| `azure.vm.memory.available` | `By` | Virtual machine, `Available Memory Bytes` |
| `azure.app_service.plan.cpu.utilization` | `1` | App Service Plan, `CpuPercentage` (divided by 100) |
| `azure.app_service.plan.memory.utilization` | `1` | App Service Plan, `MemoryPercentage` (divided by 100) |
| `azure.sql.database.cpu.utilization` | `1` | SQL database, `cpu_percent` (divided by 100) |
| `azure.sql.database.storage.utilization` | `1` | SQL database, `storage_percent` (divided by 100) |

## Resource attributes

Every series of a resource carries:

| Attribute | Value |
|---|---|
| `cloud.provider` | `azure` |
| `cloud.account.id` | The subscription ID |
| `cloud.region` | The ARM location |
| `cloud.resource_id` | The ARM id **exactly as Resource Graph returns it** (casing included), for joins with Azure's own telemetry |
| `azure.resource_group.name` | The resource group |
| `azure.resource_provider.namespace` | The provider namespace |
| `azure.resource.type` | The ARM type |
| `azure.resource.name` | The resource name |
| `azure.tenant.id` | The tenant |
| `cloud.platform` | Only for virtual machines, App Service, Functions, AKS, Container Apps and Container Instances |
| `azure.resource.tag.<name>` | Only the tags listed in `tags_allow` |

## State

| Metric | Unit | Attribute | Description |
|---|---|---|---|
| `azure.resource.provisioning.status` | `1` | `azure.resource.provisioning.state` | One point per state (ARM values, lower case); `1` for the current state, `0` for the others |
| `azure.resource.status` | `1` | `azure.resource.state` | One point per product state (power state of a VM, for example, without the `PowerState/` prefix); `1` for the current one. A state that was left reads `0` instead of vanishing |

## Discovery

| Metric | Unit | Attributes | Description |
|---|---|---|---|
| `azure.monitor.discovery.excluded.count` | `{resource}` | `cloud.account.id`, `azure.resource.type` | Resources a filter left out, per subscription and ARM type. A filter is never silent |

## Plugins

| Metric | Unit | Attributes | Description |
|---|---|---|---|
| `azure.servicebus.queue.message.count` | `{message}` | `azure.servicebus.message.state` (`active`, `dead_letter`, `scheduled`, `transfer`, `transfer_dead_letter`), `messaging.system`, `messaging.destination.name`, `messaging.destination.subscription.name` (topic subscriptions), `server.address` | Messages per queue, and per topic subscription, by state. Needs `Reader` on the namespace |
| `azure.app_service.plan.usage` | per usage | `azure.app_service.plan.usage.name` | Current value of each usage the plan reports |
| `azure.app_service.plan.limit` | per usage | `azure.app_service.plan.usage.name` | Limit of that usage, when the plan reports one |
| `azure.vm.disk.free` | `By` | `host.name`, `azure.vm.disk.mount` | Free space per disk, from the `InsightsMetrics` table of the workspace; for machines that report to Log Analytics through VM Insights. Needs `Log Analytics Reader` on the workspace |

# Entities

Entity emission is **off by default**. With `entities.enabled: true` the probe publishes one entity of type `cloud.resource` per discovered resource, after the first successful discovery. Turn it on only when the topology backend you send to registers the `cloud.resource` entity type: a backend that does not know the type refuses the records.

- **Identity**: the ARM id in **lower case**, in the attribute `azure.resource.id`. Azure returns the same id with different casing depending on the API, and two spellings of one key would make two entities. Resource and resource group names are case-insensitive in Azure Resource Manager, so lower-casing cannot merge two distinct resources.
- **`cloud.resource_id`** is a descriptive attribute that keeps the id as Azure returned it; it is not the identity and is never lower-cased, so it still joins with Azure's own telemetry.
- Other attributes: the resource attributes above, the provisioning and product states, and for a virtual machine `azure.vm.id`, its `vmId` as a lower-case hyphenated UUID.
- **Relation**: `monitors`, from the agent instance running the discovery to each resource. It states who observes the resource and carries no impact in either direction.
- **Removal**: a resource missed by two discoveries is removed with the reason `not_discovered`; one that left the scope through a filter is removed with `unmonitored`. The liveness interval announced to the backend is three discovery intervals (45 minutes by default).

Subscription and resource group are attributes, not entities.

# Troubleshooting

A refusal from Azure (HTTP 403) is logged with the role that call needs: `the application lacks the role needed here: <role>`. The roles it names:

| Message names | Meaning |
|---|---|
| `Reader on the subscriptions to query` | Discovery was refused. The log reads `discovery refused: the application needs the Reader role on subscription <id>`. The probe keeps the resources it already followed |
| `Monitoring Reader on the resource` | The metrics of a resource were refused; assign `Monitoring Reader` at the subscription or resource group |
| `Reader on the Service Bus namespace` | The Service Bus plugin was refused |
| `Reader on the App Service Plan` | The App Service Plan plugin was refused |
| `Log Analytics Reader on the workspace` | The VM disk plugin was refused; assign the role on the workspace |

Each failure is logged once when it starts, not at every cycle. A resource that returns no metric simply publishes none; a role assigned a few minutes ago can take that long to be honoured by Azure.

If nothing is discovered, check that the role is assigned to the app registration's **service principal** at the scope of the subscription, and that the subscription IDs are the ones the identity can read. When `max_resources` truncates the list, the log says how many resources were left out.

# Outputs

The metrics are available through every configured output. For PRTG and Nagios, query the probe by its configured `name`:

```bash
curl "http://localhost:8080/api/{agentkey}/prtg/metrics/azmon"
curl "http://localhost:8080/api/{agentkey}/nagios/metrics/azmon"
```

The number of series follows the number of resources and of metrics per resource: bound the scope with `discovery` and `metrics` before widening it.
