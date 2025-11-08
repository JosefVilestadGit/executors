# Unique Executor Naming Implementation

## Overview

The deployment-controller now supports generating unique executor names for scaled deployments. This allows deploying multiple replicas of executors (like docker executors) where each instance needs a unique `COLONIES_EXECUTOR_NAME`.

## Implementation

### 1. New Field in DeploymentSpec

Added `executorName` field to specify the base executor name:

```json
{
  "spec": {
    "executorName": "dev-docker",
    "replicas": 3
  }
}
```

### 2. Hash-Based Unique Naming

When `executorName` is specified, the reconciler generates unique names using 5-character hex hashes:

- Format: `{baseName}-{hash}`
- Example: `dev-docker-a3f9e`, `dev-docker-7b2c1`, `dev-docker-9d4fa`
- Hash length: 5 hex characters (1,048,576 combinations)
- Collision handling: Retries up to 10 times if name already exists

### 3. Collision Detection

The reconciler checks if executor names are already registered in the colony:

```go
func (r *Reconciler) isExecutorNameTaken(colonyName, executorName string) (bool, error) {
    executor, err := r.client.GetExecutor(colonyName, executorName, r.executorPrvKey)
    if err != nil {
        // If error is "not found", name is available
        return false, nil
    }
    return executor != nil, nil
}
```

### 4. Automatic Name Generation

When starting containers, the reconciler:

1. Generates a unique hash
2. Checks if `{baseName}-{hash}` is available in the colony
3. Retries if collision occurs
4. Sets `COLONIES_EXECUTOR_NAME` environment variable

```go
if spec.ExecutorName != "" {
    uniqueExecutorName, err := r.generateUniqueExecutorName(colonyName, spec.ExecutorName)
    if err != nil {
        return fmt.Errorf("failed to generate unique executor name: %w", err)
    }
    envVars = append(envVars, "COLONIES_EXECUTOR_NAME="+uniqueExecutorName)
}
```

## Usage Example

### 1. Deploy Scaled Docker Executors

```bash
# Deploy 3 docker executor replicas
colonies service add --spec examples/docker-executor-deployment.json
```

The deployment spec:

```json
{
  "kind": "ExecutorDeployment",
  "metadata": {
    "name": "docker-executor"
  },
  "spec": {
    "image": "colonyos/dockerexecutor:v1.0.5",
    "replicas": 3,
    "executorType": "deployment-controller",
    "executorName": "dev-docker",
    "env": {
      "COLONIES_COLONY_NAME": "dev",
      "COLONIES_COLONY_PRVKEY": "...",
      ...
    },
    "volumes": [
      {
        "host": "/var/run/docker.sock",
        "container": "/var/run/docker.sock"
      }
    ],
    "privileged": true
  }
}
```

### 2. Result

Three docker executors will register with unique names:
- `dev-docker-a3f9e`
- `dev-docker-7b2c1`
- `dev-docker-9d4fa`

Each can independently process jobs submitted to the colony.

### 3. Verify

```bash
# Check registered executors
colonies executor ls

# Check running containers
docker ps --filter "label=colonies.deployment=docker-executor"
```

## Current Limitations

### 1. No Prefix Matching Yet

Currently, to target these executors you need to:

**Option A**: Don't specify executor names (jobs go to any available executor)
```json
{
  "conditions": {
    "executortype": "container-executor"
  }
}
```

**Option B**: Specify exact executor name (targets one specific instance)
```json
{
  "conditions": {
    "executornames": ["dev-docker-a3f9e"]
  }
}
```

### 2. Future Enhancement: Prefix Matching

When prefix matching is implemented in the colonies server, you'll be able to:

```json
{
  "conditions": {
    "executornames": ["dev-docker"]
  }
}
```

This will match ALL executors starting with `dev-docker-*`, distributing work across all replicas.

## Benefits

1. ✅ **No Server Changes Required**: Works with existing colonies server
2. ✅ **Collision-Free**: Checks for name availability before use
3. ✅ **Scalable**: Supports up to 100K+ executors safely
4. ✅ **Clean Deployment**: No command overrides or hacks needed
5. ✅ **Optional**: Only activates when `executorName` is specified

## Architecture

```
Reconciler
  ├─> Parse DeploymentSpec
  ├─> Check if executorName is set
  │   ├─> Generate unique hash (5 chars)
  │   ├─> Check if name exists in colony
  │   └─> Retry if collision (max 10 times)
  └─> Set COLONIES_EXECUTOR_NAME env var

Docker Executor Container
  ├─> Reads COLONIES_EXECUTOR_NAME from environment
  ├─> Registers with colony using unique name
  └─> Starts processing jobs
```

## Files Modified

1. **`pkg/reconciler/reconciler.go`**:
   - Added `ExecutorName` field to `DeploymentSpec`
   - Added `generateUniqueHash()` function
   - Added `isExecutorNameTaken()` method
   - Added `generateUniqueExecutorName()` method
   - Modified `startContainer()` to generate and set unique names

2. **`examples/docker-executor-deployment.json`**:
   - Clean deployment spec using `executorName` field
   - No command overrides needed

## Testing

```bash
# 1. Rebuild and restart deployment-controller
cd /home/johan/dev/github/colonyos/executors/deployment-controller
make container
docker-compose restart deployment-controller

# 2. Create deployment
colonies service add --spec examples/docker-executor-deployment.json

# 3. Watch logs
docker-compose logs -f deployment-controller

# 4. Verify executors registered
colonies executor ls

# 5. Check running containers
docker ps --filter "label=colonies.deployment=docker-executor"
```

## Next Steps

Once this is working, we can implement prefix matching in the colonies server:

1. Modify `pkg/database/postgresql/processes.go` to add `FindCandidatesByPrefix()`
2. Update `pkg/scheduler/scheduler.go` to try prefix matching after exact match
3. This will enable `"executornames": ["dev-docker"]` to match all scaled instances

---

**Date**: 2025-11-07
**Status**: Implemented, ready for testing
