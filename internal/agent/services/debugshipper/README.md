# DebugLogShipper

The DebugLogShipper is a flexible solution for shipping debug logs to remote endpoints. It
implements the `io.Writer` interface, making it compatible with Zerolog and other Go logging libraries.

This is a support tool, meant to be switched on temporarily while diagnosing
a host. The endpoint must use `https://`. Plain `http://` is accepted only for
a loopback destination (`localhost`, `127.0.0.0/8`, `::1`); any other `http://`
URL is refused at start-up with an explicit error. No credential is required.

## Features

- Buffer and batch logs to reduce network traffic
- Periodically flush logs to remote endpoints
- Compatible with any HTTP-based log collection system
- Add custom headers and tags to logs
- Configurable buffer size and flush intervals
- Clean shutdown with final flush of pending logs

## Usage

### Command Line Options

To activate the DebugLogShipper, use the following command line flags:

```bash
# Basic usage
senhub-agent start --authentication-key=MYKEY --debug-log-shipper-url=https://logserver:9428/api/v1/write

# With tags 
senhub-agent start --authentication-key=MYKEY \
    --debug-log-shipper-url=https://logserver:9428/api/v1/write \
    --debug-log-shipper-tags=env=production,component=agent

# Custom buffer size
senhub-agent start --authentication-key=MYKEY \
    --debug-log-shipper-url=https://logserver:9428/api/v1/write \
    --debug-log-shipper-buffer=200
```

### Environment Variables

You can also configure the DebugLogShipper using environment variables:

```bash
export SENHUB_KEY=MYKEY
export SENHUB_DEBUG_LOG_SHIPPER_URL=https://logserver:9428/api/v1/write
export SENHUB_DEBUG_LOG_SHIPPER_TAGS=env=production,component=agent
export SENHUB_DEBUG_LOG_SHIPPER_BUFFER=200

senhub-agent start
```

## Integration with Log Systems

The DebugLogShipper can be used with various log collection systems:

### VictoriaLogs

For VictoriaLogs, the DebugLogShipper uses the JSON Stream API. Just specify the base URL and the shipper will automatically add the proper endpoint and query parameters:

```
https://victorialogs:9428
```

The shipper will convert this to:
```
https://victorialogs:9428/insert/jsonline?_stream_fields=stream&_time_field=timestamp&_msg_field=message
```

### Loki 

For Grafana Loki, use an endpoint like:
```
https://loki:3100/loki/api/v1/push
```

### Elasticsearch

For Elasticsearch, use an endpoint like:
```
https://elasticsearch:9200/logs/_doc
```

### Custom HTTP Endpoints

Any HTTP endpoint that accepts JSON logs can be used. The DebugLogShipper sends logs as newline-delimited JSON objects.

## Advanced Configuration

For advanced configuration needs, you can extend the DebugLogShipper in code:

```go
import (
    "senhub-agent.go/internal/agent/services/debugshipper"
)

// Create a custom configuration
config := debugshipper.DefaultConfig()
config.Endpoint = "http://logserver:9428/api/v1/write"
config.BufferSize = 200
config.FlushInterval = 5 * time.Second
config.Headers = map[string]string{
    "Content-Type": "application/json",
    "X-Custom-Header": "value",
}

// Create the shipper
shipper, err := debugshipper.NewDebugLogShipper(config)
if err != nil {
    // Handle error
}
defer shipper.Close()

// Use with zerolog
logger := zerolog.New(shipper).With().Timestamp().Logger()
```