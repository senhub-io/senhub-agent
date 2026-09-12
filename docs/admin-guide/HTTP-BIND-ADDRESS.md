# HTTP Strategy Bind Address Configuration

The HTTP strategy supports configurable bind addresses, allowing you to control which network interface the HTTP server listens on.

## Configuration Parameter

- **`bind_address`**: String specifying the IP address to bind to
  - Default: `"127.0.0.1"` (loopback only). Reaching the endpoints from another
    machine — a PRTG probe, a Prometheus scraper — is an explicit opt-in.
  - Accepts any address the host can bind (IPv4 or IPv6)

The examples below use the monolithic `agent-config.yaml` layout. In the
multi-file layout the same parameters live in `strategies.d/00-http.yaml`
under a single top-level `http:` key:

```yaml
http:
  port: 8080
  bind_address: "127.0.0.1"
  endpoints: ["prtg", "web", "nagios"]
```

## Common Use Cases

### 1. Localhost Only (Default)
```yaml
storage:
  - name: http
    params:
      port: 8080
      bind_address: "127.0.0.1"
```
- Only accessible from the local machine
- The behaviour when `bind_address` is omitted
- Increased security for internal monitoring

### 2. Specific Network Interface
```yaml
storage:
  - name: http
    params:
      port: 8080
      bind_address: "192.168.1.100"
```
- Bind to a specific network interface
- Useful in multi-homed servers
- Control which network segment can access the API

### 3. All Interfaces
```yaml
storage:
  - name: http
    params:
      port: 8080
      bind_address: "0.0.0.0"
```
- Accessible from any network interface
- Must be set explicitly: the default is `127.0.0.1`
- Maximum accessibility — pair it with firewall rules

## Security Considerations

1. **Localhost Binding**: Use `127.0.0.1` when the HTTP API should only be accessible locally
2. **Network Segmentation**: Use specific IP addresses to control access from different network segments
3. **Firewall Rules**: Always complement bind address configuration with appropriate firewall rules
4. **Agent Key**: Remember that the API still requires a valid agent key for authentication

## Examples by Environment

### Development Environment
```yaml
storage:
  - name: http
    params:
      port: 8080
      bind_address: "127.0.0.1"  # Local access only
```

### Production Environment with DMZ
```yaml
storage:
  - name: http
    params:
      port: 8080
      bind_address: "10.0.1.50"  # DMZ interface only
```

### Multi-Interface Monitoring
```yaml
storage:
  - name: http
    params:
      port: 8080
      bind_address: "127.0.0.1"  # Internal monitoring
  - name: http
    params:
      port: 8081
      bind_address: "192.168.1.100"  # External monitoring
```

## Validation

The configuration is validated at startup:
- `bind_address` must be a string
- Invalid or unavailable addresses are caught when the listener is opened
- Configuration errors are logged with detailed error messages
- `senhub-agent config check` reports the same errors before a restart

## Logging

The HTTP strategy logs the bind address and port on startup. The log file is
human-readable text by default; the JSON form below is what `--log-format json`
writes:

```
{"level":"info","port":8080,"bind_address":"127.0.0.1","message":"Starting HTTP strategy"}
{"level":"info","address":"127.0.0.1:8080","port":8080,"bind_address":"127.0.0.1","message":"HTTP server listening"}
```