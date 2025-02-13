# SenHub agent

## Install for development

```bash
make install
```

## Running the project

You need to have go installed on your machine.
At the time of writing, the project is using go 1.23.2

To run the project in development mode, you need to run the following command:

```bash
make watch
```

This will start the project in development mode, and it will watch for changes
in the code.

To run the project in production mode, you need to run the following command:

```bash
make build
./senhub-agent start --authentication-key some_key --server-url "http://localhost:8080"
```

### Build environment

Project can be built in `development` or `production` mode by setting `ENV`
variable.

```bash
ENV=development make build
```

## Running the tests

To run the tests, you need to run the following command:

```bash
make test
```

## Configuration

Agent configuration is read from senhub server.
A valid configuration mathes the following structure:

```json
{
  "agent": {
    "version": "0.1.0",
    "registry_url": "https://eu-west-1.intake.senhub.io/"
  },
  "probes": [
    {
      "name": "load_webapp",
      "params": { "url": "http://www.google.fr", "timeout": 5 }
    },
    { "name": "ping_webapp", "params": { "url": "http://example.org:8080" } },
    { "name": "ping_gateway", "params": {} },
    { "name": "wifi_signal_strength", "params": {} },
    { "name": "memory", "params": {} }
  ],
  "storage": [
    { "name": "senhub", "params": {} },
    {
      "name": "prtg",
      "params": {
        "data_retention_period": "2m",
        "server_url": "http://localhost:8080"
      }
    }
  ]
}
```

### Agent

- `version` (optional): required version for the agent. Can be in the form of `x.y.z`,
  `latest`, `>=x.y.z`, `<=x.y.z`, `>x.y.z`, `<x.y.z`, `!=x.y.z`
- `registry_url` (optional): URL to the registry server, default is
  `https://eu-west-1.intake.senhub.io/`
