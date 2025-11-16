# Reconciliation Testing Framework

This document describes the testing framework for the docker-reconciler's reconciliation mechanism.

## Overview

The testing framework provides comprehensive scenario-based testing for the reconciliation mechanism, allowing you to:
- Test corner cases and edge scenarios
- Validate behavior with large numbers of containers
- Verify concurrent reconciliation operations
- Test drift detection and self-healing mechanisms
- Simulate container failures and state changes

## Test Files

### 1. reconciliation_scenarios_test.go
Contains practical, real-world test scenarios that validate reconciliation behavior:

#### Test Scenarios

**TestReconciliationScenario_RapidScaleUpDown**
- Tests rapid scaling from 0 → 10 → 5 containers
- Validates scale-up and scale-down operations
- Verifies correct container count after scaling

**TestReconciliationScenario_GenerationRollover**
- Tests handling of multiple container generations
- Simulates mixed generations (old: 1, 3; current: 5)
- Validates drift detection for old generation containers

**TestReconciliationScenario_LargeScaleDeployment**
- Tests with 100 containers simultaneously
- Validates reconciler performance at scale
- Skipped in short test mode (use `-short` flag)

**TestReconciliationScenario_PartialFailures**
- Tests handling of mixed container states
- Simulates 3 running + 2 stopped containers
- Validates accurate status collection

**TestReconciliationScenario_DriftDetection**
- **ReplicaDrift_TooFew**: Tests detection when running < desired (5 vs 10)
- **ReplicaDrift_TooMany**: Tests detection when running > desired (10 vs 5)
- Validates drift detection accuracy

**TestReconciliationScenario_ConcurrentStatusChecks**
- Tests 10 concurrent status checks
- Validates thread-safety of reconciliation operations
- Ensures no race conditions during concurrent access

### 2. reconciler_integration_test.go
Contains integration tests with mocked Docker and Colonies clients:

**TestCleanupStoppedContainers**
- Tests removal of stopped containers
- Validates selective cleanup (stopped only, not running)
- Tests error handling during cleanup

**TestCollectStatus**
- Tests status collection from containers
- Validates accurate counting of running/stopped instances
- Tests handling of empty container lists

**TestScaleDownDeregistration**
- Tests executor deregistration before container stop
- Validates correct operation order
- Tests graceful handling of deregistration failures

## Running Tests

### Run All Reconciliation Scenario Tests
```bash
go test -v ./pkg/reconciler -run TestReconciliationScenario
```

### Run Specific Scenario
```bash
go test -v ./pkg/reconciler -run TestReconciliationScenario_RapidScaleUpDown
go test -v ./pkg/reconciler -run TestReconciliationScenario_DriftDetection
```

### Run Large-Scale Tests (including 100 containers test)
```bash
go test -v ./pkg/reconciler -run TestReconciliationScenario_LargeScale
```

### Run All Reconciler Tests
```bash
go test -v ./pkg/reconciler
```

### Skip Long-Running Tests
```bash
go test -v -short ./pkg/reconciler
```

## Creating Custom Scenarios

You can easily add new test scenarios by following this pattern:

```go
func TestReconciliationScenario_YourScenario(t *testing.T) {
    mockDocker := new(MockDockerClient)
    reconciler := createTestReconciler(mockDocker)

    blueprint := &core.Blueprint{
        Kind: "ExecutorDeployment",
        Metadata: core.BlueprintMetadata{
            Name:       "your-test",
            Generation: 1,
        },
        Spec: map[string]interface{}{
            "replicas": float64(5),
            "image":    "test:latest",
        },
    }

    // Setup container state
    containers := make([]types.Container, 5)
    for i := 0; i < 5; i++ {
        containers[i] = types.Container{
            ID:    generateContainerID(i),
            Names: []string{generateContainerName("your-test", i)},
            State: "running",
            Labels: map[string]string{
                "colonies.managed":    "true",
                "colonies.deployment": "your-test",
                "colonies.generation": "1",
            },
        }

        // Mock ContainerInspect for status collection
        mockDocker.On("ContainerInspect", mock.Anything, generateContainerID(i)).
            Return(types.ContainerJSON{
                ContainerJSONBase: &types.ContainerJSONBase{
                    ID:    generateContainerID(i),
                    Name:  generateContainerName("your-test", i),
                    State: &types.ContainerState{Running: true},
                },
                Config: &container.Config{
                    Image:  "test:latest",
                    Labels: map[string]string{"colonies.generation": "1"},
                },
            }, nil)
    }

    mockDocker.On("ContainerList", mock.Anything, mock.Anything).Return(containers, nil)

    // Execute test
    status, err := reconciler.CollectStatus(blueprint)
    assert.NoError(t, err)
    assert.Equal(t, 5, status["runningInstances"])

    mockDocker.AssertExpectations(t)
}
```

## Exploring Corner Cases

### Example Corner Cases to Test

1. **Zero-to-Many Scale Up**
   ```go
   // Start: 0 containers
   // Target: 50 containers
   // Validate: All containers created successfully
   ```

2. **Generation Lag**
   ```go
   // Scenario: Containers from generations 1, 2, 3, 4, 5
   // Current generation: 5
   // Validate: Old generations detected and marked for cleanup
   ```

3. **Partial Reconciliation Failure**
   ```go
   // Scenario: 5/10 containers fail to start
   // Validate: Status accurately reflects 5 running, 5 failed
   ```

4. **Rapid Blueprint Changes**
   ```go
   // Scenario: Generation changes every second
   // Validate: Reconciler handles rapid updates correctly
   ```

5. **Mixed State Containers**
   ```go
   // Scenario: running, stopped, exited, restarting
   // Validate: Accurate state detection and reporting
   ```

## Benchmarking

Run benchmarks to measure reconciliation performance:

```bash
# Run all benchmarks
go test -bench=. ./pkg/reconciler

# Run specific benchmark
go test -bench=BenchmarkContainerList ./pkg/reconciler

# With memory profiling
go test -bench=. -benchmem ./pkg/reconciler
```

## Mock Architecture

### MockDockerClient
Simulates Docker daemon operations:
- `ContainerList`: Returns simulated container state
- `ContainerInspect`: Returns detailed container info
- `ContainerCreate/Start/Stop/Remove`: Simulates lifecycle operations
- `ImagePull/ImageInspect`: Simulates image operations

### MockColoniesClient
Simulates Colonies server operations:
- `AddExecutor`: Simulates executor registration
- `RemoveExecutor`: Simulates executor deregistration
- `ApproveExecutor`: Simulates executor approval
- `AddLog/Close`: Simulates process logging and completion

## Best Practices

1. **Always mock ContainerInspect** when containers are returned by ContainerList
2. **Use `.Once()` or `.Times(n)`** to specify exact call expectations
3. **Test both success and failure paths** for robust validation
4. **Use sub-tests** (t.Run) to organize related scenarios
5. **Add descriptive log messages** (t.Log) to explain what's being tested
6. **Clean up mocks** with `mockDocker.AssertExpectations(t)` at test end

## Integration with Self-Healing

The scenario tests can be extended to test the self-healing mechanism:

```go
func TestSelfHealing_DriftCorrection(t *testing.T) {
    // 1. Setup initial state with drift
    // 2. Trigger self-healing check
    // 3. Verify drift detection
    // 4. Verify reconciliation triggered
    // 5. Verify drift corrected
}
```

## Continuous Integration

Add to your CI pipeline:

```yaml
# .github/workflows/test.yml
- name: Run Reconciliation Tests
  run: go test -v ./pkg/reconciler -race -coverprofile=coverage.out

- name: Run Large-Scale Tests
  run: go test -v ./pkg/reconciler -run TestReconciliationScenario_LargeScale
```

## Troubleshooting

### Common Issues

**Mock not returning data**
- Ensure ContainerInspect is mocked for all containers returned by ContainerList
- Check that mock expectations match actual function calls

**Race conditions in tests**
- Use `-race` flag to detect races: `go test -race ./pkg/reconciler`
- Ensure proper synchronization in concurrent tests

**Tests timing out**
- Increase timeout: `go test -timeout 60s ./pkg/reconciler`
- Check for deadlocks in concurrent operations

## Future Enhancements

Potential additions to the testing framework:

1. **Chaos Engineering Simulator** (reconciler_simulator_test.go)
   - Random container crashes
   - Network failures
   - Resource exhaustion
   - Docker daemon disconnects

2. **Performance Profiling**
   - CPU and memory profiling
   - Bottleneck identification
   - Optimization targets

3. **Fuzzing**
   - Random blueprint generation
   - Random state transitions
   - Edge case discovery

4. **Load Testing**
   - Sustained high load
   - Burst traffic patterns
   - Resource limits testing
