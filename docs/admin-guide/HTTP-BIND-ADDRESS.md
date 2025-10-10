# HTTP Strategy Bind Address Configuration

The HTTP strategy now supports configurable bind addresses, allowing you to control which network interface the HTTP server listens on.

## Configuration Parameter

- **`bind_address`**: String specifying the IP address to bind to
  - Default: `"0.0.0.0"` (all interfaces)
  - Accepts any valid IPv4 address

## Common Use Cases

### 1. Localhost Only (Security)
```yaml
storage:
  - type: "http"
    config:
      port: 8080
      bind_address: "127.0.0.1"
```
- Only accessible from the local machine
- Increased security for internal monitoring
- Useful for development or secure environments

### 2. Specific Network Interface
```yaml
storage:
  - type: "http"
    config:
      port: 8080
      bind_address: "192.168.1.100"
```
- Bind to a specific network interface
- Useful in multi-homed servers
- Control which network segment can access the API

### 3. All Interfaces (Default)
```yaml
storage:
  - type: "http"
    config:
      port: 8080
      bind_address: "0.0.0.0"
```
- Accessible from any network interface
- Default behavior if `bind_address` is not specified
- Maximum accessibility

## Security Considerations

1. **Localhost Binding**: Use `127.0.0.1` when the HTTP API should only be accessible locally
2. **Network Segmentation**: Use specific IP addresses to control access from different network segments
3. **Firewall Rules**: Always complement bind address configuration with appropriate firewall rules
4. **Agent Key**: Remember that the API still requires a valid agent key for authentication

## Examples by Environment

### Development Environment
```yaml
storage:
  - type: "http"
    config:
      port: 8080
      bind_address: "127.0.0.1"  # Local access only
```

### Production Environment with DMZ
```yaml
storage:
  - type: "http"
    config:
      port: 8080
      bind_address: "10.0.1.50"  # DMZ interface only
```

### Multi-Interface Monitoring
```yaml
storage:
  - type: "http"
    config:
      port: 8080
      bind_address: "127.0.0.1"  # Internal monitoring
  - type: "http"
    config:
      port: 8081
      bind_address: "192.168.1.100"  # External monitoring
```

## Validation

The configuration is validated at startup:
- `bind_address` must be a string
- Invalid IP addresses will be caught by the Go net package during server startup
- Configuration errors are logged with detailed error messages

## Logging

The HTTP strategy logs the bind address and port on startup:
```
{"level":"info","port":8080,"bind_address":"127.0.0.1","message":"Starting HTTP strategy"}
{"level":"info","address":"127.0.0.1:8080","port":8080,"bind_address":"127.0.0.1","message":"HTTP server listening"}
```