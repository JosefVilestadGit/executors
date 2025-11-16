# Debugging Docker Reconciler

This guide provides comprehensive debugging strategies for the Docker Reconciler.

## Table of Contents
- [Quick Debugging Commands](#quick-debugging-commands)
- [Real-time Monitoring](#real-time-monitoring)
- [Common Issues](#common-issues)
- [Detailed Investigation](#detailed-investigation)
- [Blueprint Status](#blueprint-status)

## Quick Debugging Commands

### Check Reconciler Status
```bash
# Check if reconciler is running
docker ps | grep docker-reconciler

# View recent reconciler logs
docker logs docker-reconciler 2>&1 | tail -50

# View logs with timestamps
docker logs -t docker-reconciler 2>&1 | tail -50
```

### Check Blueprint Status
```bash
# List all blueprints
colonies blueprint ls

# Get detailed blueprint status
colonies blueprint get --name <blueprint-name>

# Check reconciliation process status
colonies process get --processid <process-id>
```

## Real-time Monitoring

### Watch Reconciler Logs Live

**Best practice for debugging reconciliation:**

Open one terminal with live logs:
```bash
docker logs -f docker-reconciler
```

Then in another terminal, make your changes:
```bash
# Update blueprint
colonies blueprint update --spec myblueprint.json

# Or add new blueprint
colonies blueprint add --spec myblueprint.json
```

You'll see the reconciliation happen in real-time in the first terminal.

### Follow Specific Blueprint
```bash
# Watch for specific blueprint name
docker logs -f docker-reconciler 2>&1 | grep --line-buffered "myblueprint"
```

## Common Issues

### 1. Port Already in Use

**Symptom:**
```
Error: driver failed programming external connectivity: bind: address already in use
```

**Solution:**
```bash
# Check what's using the port
sudo netstat -tlnp | grep :8080

# Or use ss
ss -tlnp | grep :8080

# Find and stop the conflicting container
docker ps | grep 8080
docker stop <container-id>

# Or update your blueprint to use a different port
```

### 2. Image Pull Failed

**Symptom:**
```
Error: failed to pull image: manifest not found
```

**Solution:**
```bash
# Verify image exists
docker pull nginx:alpine

# Check reconciler has access to Docker registry
docker exec docker-reconciler docker images

# For private registries, check authentication
docker exec docker-reconciler cat ~/.docker/config.json
```

### 3. Reconciliation Stuck

**Symptom:**
Blueprint shows "reconciling" for extended time.

**Solution:**
```bash
# Check reconciliation process status
colonies blueprint get --name <blueprint-name>

# Get the Last Reconciliation process ID, then:
colonies process get --processid <process-id>

# Check if reconciler is receiving processes
docker logs docker-reconciler 2>&1 | grep "Assigning process"

# Restart reconciler if stuck
docker restart docker-reconciler
```

### 4. Containers Not Starting

**Symptom:**
`runningInstances` stays at 0/N

**Investigation:**
```bash
# Check Docker daemon logs
journalctl -u docker -n 50

# Check for resource constraints
docker stats

# Check reconciler logs for errors
docker logs docker-reconciler 2>&1 | grep -i error

# Inspect failed containers
docker ps -a | grep <blueprint-name>
docker logs <container-id>
```

## Detailed Investigation

### Get Full Reconciliation History
```bash
# Get blueprint with process history
colonies blueprint get --name <blueprint-name>

# Get logs from specific reconciliation
colonies log get --processid <process-id>

# Get full reconciliation history
colonies blueprint history --name <blueprint-name>
```

### Search Logs Across All Reconciliations
```bash
# Search for errors in reconciler logs
docker logs docker-reconciler 2>&1 | grep -i "error"

# Search for specific blueprint
docker logs docker-reconciler 2>&1 | grep -A 20 "<blueprint-name>"

# Search for failed processes
docker logs docker-reconciler 2>&1 | grep "Process failed"

# Search colony logs
colonies log search --text "error"
colonies log search --text "failed"
```

### Inspect Running Containers
```bash
# List all containers created by reconciler
docker ps -a --filter "label=blueprint"

# Inspect specific container
docker inspect <container-id>

# Check container logs
docker logs <container-id>

# Execute into running container
docker exec -it <container-id> sh
```

## Blueprint Status

### Understanding Blueprint Output

When you run `colonies blueprint get --name <name>`:

**Key Fields:**
- **Generation**: Version number, increments with each spec change
- **Last Reconciliation**: Process ID of most recent reconciliation attempt
- **Reconciliation Status**: SUCCESS, FAILED, or RUNNING
- **Reconciliation Time**: When reconciliation started
- **Reconciliation Ended**: When reconciliation completed

**Status Section:**
```bash
# If status shows:
runningInstances: 0
totalInstances: 3

# This means 0 out of 3 desired containers are running
# Check reconciliation logs to see why
```

### Reconciliation States

**RUNNING**: Reconciliation in progress
- Check live logs: `docker logs -f docker-reconciler`

**SUCCESS**: Reconciliation completed successfully
- Verify containers: `docker ps | grep <blueprint-name>`

**FAILED**: Reconciliation failed
- Get error: `colonies log get --processid <process-id>`
- Check reconciler: `docker logs docker-reconciler 2>&1 | tail -100`

## Debugging Workflow

### Step-by-Step Debugging Process

1. **Check Blueprint Status**
   ```bash
   colonies blueprint ls
   colonies blueprint get --name <blueprint-name>
   ```

2. **Check Reconciler Health**
   ```bash
   docker ps | grep docker-reconciler
   docker logs docker-reconciler 2>&1 | tail -50
   ```

3. **Review Recent Reconciliation**
   ```bash
   # Get process ID from blueprint get output
   colonies log get --processid <process-id>
   ```

4. **Check Docker Environment**
   ```bash
   # Check running containers
   docker ps

   # Check for failed containers
   docker ps -a | grep <blueprint-name>

   # Check Docker resources
   docker stats --no-stream
   ```

5. **Search for Errors**
   ```bash
   # In reconciler
   docker logs docker-reconciler 2>&1 | grep -i error

   # In container logs
   docker logs <container-id> 2>&1 | grep -i error
   ```

6. **Test Manually**
   ```bash
   # Try starting container manually to isolate issue
   docker run --rm nginx:alpine
   ```

## Performance Debugging

### Check Reconciler Performance
```bash
# Monitor reconciler resource usage
docker stats docker-reconciler

# Check reconciliation timing
docker logs docker-reconciler 2>&1 | grep "Reconciliation completed"

# Count active reconciliations
docker logs docker-reconciler 2>&1 | grep "Starting reconciliation" | wc -l
```

### Check Docker Performance
```bash
# Check overall Docker performance
docker stats --no-stream

# Check disk usage
docker system df

# Clean up if needed
docker system prune
```

## Verbose Logging

### Enable Debug Logging

If reconciler was built with verbose flag support:
```bash
# Restart with verbose logging
docker stop docker-reconciler
docker run -d --name docker-reconciler \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -e VERBOSE=true \
  colonyos/docker-reconciler:latest
```

### Colony Client Verbose Mode
```bash
# Use verbose flag for detailed output
colonies blueprint get --name <blueprint-name> --verbose
colonies log get --processid <process-id> --verbose
```

## Getting Help

When asking for help, provide:

1. Blueprint definition
   ```bash
   colonies blueprint get --name <blueprint-name> --json
   ```

2. Recent reconciler logs
   ```bash
   docker logs docker-reconciler 2>&1 | tail -100
   ```

3. Reconciliation process logs
   ```bash
   colonies log get --processid <process-id>
   ```

4. Docker environment info
   ```bash
   docker version
   docker info
   docker ps -a
   ```

5. Blueprint list
   ```bash
   colonies blueprint ls
   ```
