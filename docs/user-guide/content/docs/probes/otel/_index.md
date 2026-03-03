---
title: "OpenTelemetry"
weight: 14
---

{{< hint danger >}}
**License: Enterprise** - Requires an Enterprise license. See [License Tiers]({{< relref "/docs/configuration#license-tiers" >}}).
{{< /hint >}}

{{< hint info >}}
**Online mode only** - This probe requires an active connection to the SenHub Observability Platform. It is not available in offline mode.
{{< /hint >}}

# OpenTelemetry Probe

## Introduction

The OpenTelemetry probe is a SenHub Agent component that receives telemetry data (metrics, traces, logs) from applications instrumented with OpenTelemetry. It supports both HTTP and gRPC protocols for data collection, providing maximum flexibility for integration into different architectures.

### Key Features

- OTLP (OpenTelemetry Protocol) data reception
- HTTP and gRPC protocol support
- Simultaneous collection of metrics, traces, and logs
- Flexible endpoint and authentication configuration
- TLS support for secure communications
- Seamless integration with the SenHub metrics system

## Probe Configuration

### Basic Configuration

Here is a basic configuration example for the OpenTelemetry probe in the SenHub Agent configuration file:

```yaml
probes:
  - name: otel
    interval: 60  # collection interval in seconds
    telemetry_types:
      - metrics
      - traces
      - logs
```

### HTTP Collector Configuration

```yaml
probes:
  - name: otel
    interval: 60
    http:
      endpoint: "http://localhost:4318"
      timeout: 30  # timeout in seconds
      headers:
        Content-Type: "application/json"
        Authorization: "Bearer <token>"
      telemetry_types:
        - metrics
        - logs
```

### gRPC Collector Configuration

```yaml
probes:
  - name: otel
    interval: 60
    grpc:
      endpoint: "localhost:4317"
      timeout: 30  # timeout in seconds
      insecure: false  # use secure TLS connection (true to disable TLS)
      telemetry_types:
        - metrics
        - traces
```

### Advanced Configuration (HTTP and gRPC in Parallel)

```yaml
probes:
  - name: otel
    interval: 60
    telemetry_types:
      - metrics
      - traces
      - logs
    http:
      endpoint: "http://localhost:4318"
      timeout: 30
      telemetry_types:
        - metrics
        - logs
    grpc:
      endpoint: "localhost:4317"
      timeout: 30
      insecure: false
      telemetry_types:
        - traces
```

## Collected Metrics

The OpenTelemetry probe can collect all metrics sent by applications instrumented with OpenTelemetry. Metrics are normalized according to OpenTelemetry conventions.

### Supported Metric Types

- **Counters** - Monotonically increasing values
- **Gauges** - Values that can increase and decrease
- **Histograms** - Value distributions with buckets
- **UpDown Counters** - Counters that can increase and decrease

### Example Collected Metrics

```
Name: http.server.request.duration
Type: Histogram
Tags:
  - host: web-server-01
  - method: GET
  - route: /api/users
  - status_code: 200
```

```
Name: system.memory.usage
Type: Gauge
Tags:
  - host: app-server-02
  - state: used
```

## Security

### TLS Configuration

For gRPC connections, TLS security is enabled by default. You can configure TLS parameters as follows:

```yaml
probes:
  - name: otel
    grpc:
      endpoint: "localhost:4317"
      insecure: false  # true to disable TLS
      ca_file: "/path/to/ca.pem"  # optional - custom CA certificate
      cert_file: "/path/to/cert.pem"  # optional - client certificate
      key_file: "/path/to/key.pem"  # optional - client private key
```

### Authentication

Several authentication methods are supported:

#### Basic Auth (HTTP)

```yaml
probes:
  - name: otel
    http:
      endpoint: "http://localhost:4318"
      username: "user"
      password: "pass"
```

#### Token Authentication (HTTP & gRPC)

For HTTP:
```yaml
probes:
  - name: otel
    http:
      endpoint: "http://localhost:4318"
      headers:
        Authorization: "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
```

For gRPC:
```yaml
probes:
  - name: otel
    grpc:
      endpoint: "localhost:4317"
      token: "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
```

## Troubleshooting

### Common Issues and Solutions

1. **Connection refused**
   - Verify that the OTLP endpoint is correctly configured and active
   - Check that the port is open and accessible
   - Test the connection with `telnet <host> <port>` to verify reachability

2. **Authentication errors**
   - Verify that the credentials are correct
   - Ensure that authentication headers are properly configured
   - Check that the token has not expired

3. **TLS errors**
   - Verify that certificates are valid and not expired
   - Ensure the hostname matches the name in the certificate
   - Check that CA, cert, and key file paths are correct

4. **No data received**
   - Verify that telemetry types (metrics, traces, logs) are correctly configured
   - Check that the source application is actually sending data
   - Increase the logging level for more details

### Logging and Diagnostics

To enable detailed logging for the OpenTelemetry probe, add the following configuration:

```yaml
logging:
  level: debug
  probes:
    otel: trace  # specific log level for the OpenTelemetry probe
```

### Configuration Validation

Use the following command to validate the probe configuration:

```bash
senhub-agent validate --config path/to/config.yaml
```

## Usage Examples

### Collecting Metrics from a Web Application

```yaml
probes:
  - name: otel
    interval: 30
    telemetry_types:
      - metrics
    http:
      endpoint: "http://webapp:4318"
      timeout: 15
```

### Collecting Traces from a Microservices Architecture

```yaml
probes:
  - name: otel
    interval: 60
    telemetry_types:
      - traces
    grpc:
      endpoint: "tracing-service:4317"
      timeout: 30
      insecure: false
      token: "${OTEL_AUTH_TOKEN}"  # using an environment variable
```

### Full Collection for a Production Environment

```yaml
probes:
  - name: otel
    interval: 60
    telemetry_types:
      - metrics
      - traces
      - logs
    http:
      endpoint: "https://otel-collector.example.com:4318"
      timeout: 30
      headers:
        Authorization: "Bearer ${OTEL_TOKEN}"
      verify_ssl: true
    retry:
      max_attempts: 3
      initial_delay: 5
      max_delay: 30
      multiplier: 2
```
