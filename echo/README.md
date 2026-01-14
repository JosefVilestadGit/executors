# Echo Executor

A simple ColonyOS executor that echoes back its input. Useful for testing and demonstrating the ColonyOS executor framework.

## Building

```bash
# Build binary
make build

# Build container (single platform)
make container

# Build and push multiplatform container (amd64 + arm64)
make multiplatform-push
```

## Running

### Environment Variables

| Variable | Description | Required |
|----------|-------------|----------|
| `COLONIES_SERVER_HOST` | ColonyOS server hostname | Yes |
| `COLONIES_SERVER_PORT` | ColonyOS server port | Yes |
| `COLONIES_TLS` | Enable TLS (`true`/`false`) | Yes |
| `COLONIES_COLONY_NAME` | Colony name | Yes |
| `COLONIES_COLONY_PRVKEY` | Colony private key (for auto-registration) | Yes |
| `COLONIES_EXECUTOR_NAME` | Executor name (auto-generated if empty) | No |
| `COLONIES_EXECUTOR_PRVKEY` | Executor private key (auto-generated if empty) | No |
| `COLONIES_VERBOSE` | Enable verbose logging | No |

### Start executor

```bash
export COLONIES_SERVER_HOST=localhost
export COLONIES_SERVER_PORT=50080
export COLONIES_TLS=false
export COLONIES_COLONY_NAME=dev
export COLONIES_COLONY_PRVKEY=<your-colony-prvkey>

./bin/echo_executor start -v
```

## Usage

### Function spec (echo.json)

```json
{
    "conditions": {
        "executortype": "echo-executor"
    },
    "funcname": "echo",
    "args": [
        "hello world"
    ]
}
```

### Submit function

```bash
colonies function submit --spec ./echo.json
```

Or directly:

```bash
colonies function exec --func echo --args "hello world" --targettype echo-executor
```

## Blueprint Deployment

Deploy using the docker-reconciler:

```bash
colonies blueprint set --spec blueprint.json
colonies blueprint reconcile --name echoexecutor
```
