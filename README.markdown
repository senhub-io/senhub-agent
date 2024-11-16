# SenHub agent

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

## Running the tests

To run the tests, you need to run the following command:

```bash
make test
```
