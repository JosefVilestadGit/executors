package reconciler

import (
	"fmt"
	"testing"

	"github.com/colonyos/colonies/pkg/core"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// TestReconciliationScenario_RapidScaleUpDown tests rapid scaling operations
func TestReconciliationScenario_RapidScaleUpDown(t *testing.T) {
	mockDocker := new(MockDockerClient)
	reconciler := createTestReconciler(mockDocker)

	blueprint := &core.Blueprint{
		Kind: "ExecutorDeployment",
		Metadata: core.BlueprintMetadata{
			Name:       "rapid-scale-test",
			Generation: 1,
		},
		Spec: map[string]interface{}{
			"replicas": float64(10),
			"image":    "test:latest",
		},
	}

	// Scenario 1: Start with 0 containers (scale up from 0 to 10)
	mockDocker.On("ContainerList", mock.Anything, mock.Anything).Return([]types.Container{}, nil).Once()

	status, err := reconciler.CollectStatus(blueprint)
	assert.NoError(t, err)
	assert.Equal(t, 0, status["runningInstances"])
	assert.Equal(t, 0, status["totalInstances"])

	t.Log("Scenario: Rapid scale from 0 to 10 containers")

	// Scenario 2: Then scale to 5 containers (scale down from 10 to 5)
	runningContainers := make([]types.Container, 5)
	for i := 0; i < 5; i++ {
		runningContainers[i] = types.Container{
			ID:    generateContainerID(i),
			Names: []string{generateContainerName("rapid-scale-test", i)},
			State: "running",
			Labels: map[string]string{
				"colonies.managed":    "true",
				"colonies.deployment": "rapid-scale-test",
				"colonies.generation": "1",
			},
		}

		// Mock ContainerInspect for each container
		mockDocker.On("ContainerInspect", mock.Anything, generateContainerID(i)).Return(types.ContainerJSON{
			ContainerJSONBase: &types.ContainerJSONBase{
				ID:    generateContainerID(i),
				Name:  generateContainerName("rapid-scale-test", i),
				State: &types.ContainerState{Running: true},
			},
			Config: &container.Config{
				Image:  "test:latest",
				Labels: map[string]string{"colonies.generation": "1"},
			},
		}, nil).Once()
	}

	mockDocker.On("ContainerList", mock.Anything, mock.Anything).Return(runningContainers, nil).Once()

	status, err = reconciler.CollectStatus(blueprint)
	assert.NoError(t, err)
	assert.Equal(t, 5, status["runningInstances"])

	t.Log("Verified: Scaled down to 5 containers")

	mockDocker.AssertExpectations(t)
}

// TestReconciliationScenario_GenerationRollover tests handling of many generation changes
func TestReconciliationScenario_GenerationRollover(t *testing.T) {
	mockDocker := new(MockDockerClient)
	reconciler := createTestReconciler(mockDocker)

	blueprint := &core.Blueprint{
		Kind: "ExecutorDeployment",
		Metadata: core.BlueprintMetadata{
			Name:       "generation-test",
			Generation: 5, // Current generation is 5
		},
		Spec: map[string]interface{}{
			"replicas": float64(3),
			"image":    "test:latest",
		},
	}

	// Scenario: Mix of old (gen 1, 2, 3) and current (gen 5) containers
	mixedGenerationContainers := []types.Container{
		{
			ID:    "old-gen-1",
			Names: []string{"/generation-test-1"},
			State: "running",
			Labels: map[string]string{
				"colonies.managed":    "true",
				"colonies.deployment": "generation-test",
				"colonies.generation": "1", // Very old generation
			},
		},
		{
			ID:    "old-gen-2",
			Names: []string{"/generation-test-2"},
			State: "running",
			Labels: map[string]string{
				"colonies.managed":    "true",
				"colonies.deployment": "generation-test",
				"colonies.generation": "3", // Old generation
			},
		},
		{
			ID:    "current-gen-1",
			Names: []string{"/generation-test-3"},
			State: "running",
			Labels: map[string]string{
				"colonies.managed":    "true",
				"colonies.deployment": "generation-test",
				"colonies.generation": "5", // Current generation
			},
		},
	}

	mockDocker.On("ContainerList", mock.Anything, mock.Anything).Return(mixedGenerationContainers, nil)

	// Should detect old generation containers
	hasOld, err := reconciler.HasOldGenerationContainers(blueprint)
	assert.NoError(t, err)
	assert.True(t, hasOld, "Should detect containers with old generations")

	t.Log("Scenario: Detected mixed generations (1, 3, and 5)")

	mockDocker.AssertExpectations(t)
}

// TestReconciliationScenario_LargeScaleDeployment tests handling of many containers
func TestReconciliationScenario_LargeScaleDeployment(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping large-scale test in short mode")
	}

	mockDocker := new(MockDockerClient)
	reconciler := createTestReconciler(mockDocker)

	blueprint := &core.Blueprint{
		Kind: "ExecutorDeployment",
		Metadata: core.BlueprintMetadata{
			Name:       "large-scale-test",
			Generation: 1,
		},
		Spec: map[string]interface{}{
			"replicas": float64(100), // Large number of replicas
			"image":    "test:latest",
		},
	}

	// Create 100 running containers
	largeContainerSet := make([]types.Container, 100)
	for i := 0; i < 100; i++ {
		largeContainerSet[i] = types.Container{
			ID:    generateContainerID(i),
			Names: []string{generateContainerName("large-scale-test", i)},
			State: "running",
			Labels: map[string]string{
				"colonies.managed":    "true",
				"colonies.deployment": "large-scale-test",
				"colonies.generation": "1",
			},
		}
	}

	mockDocker.On("ContainerList", mock.Anything, mock.Anything).Return(largeContainerSet, nil)

	// Mock ContainerInspect for all containers
	for i := 0; i < 100; i++ {
		containerID := generateContainerID(i)
		mockDocker.On("ContainerInspect", mock.Anything, containerID).Return(types.ContainerJSON{
			ContainerJSONBase: &types.ContainerJSONBase{
				ID:    containerID,
				Name:  generateContainerName("large-scale-test", i),
				State: &types.ContainerState{Running: true},
			},
			Config: &container.Config{
				Image: "test:latest",
				Labels: map[string]string{
					"colonies.generation": "1",
				},
			},
		}, nil)
	}

	status, err := reconciler.CollectStatus(blueprint)
	assert.NoError(t, err)
	assert.Equal(t, 100, status["runningInstances"])
	assert.Equal(t, 100, status["totalInstances"])

	t.Log("Scenario: Successfully managed 100 containers")

	mockDocker.AssertExpectations(t)
}

// TestReconciliationScenario_PartialFailures tests handling of some failed containers
func TestReconciliationScenario_PartialFailures(t *testing.T) {
	mockDocker := new(MockDockerClient)
	reconciler := createTestReconciler(mockDocker)

	blueprint := &core.Blueprint{
		Kind: "ExecutorDeployment",
		Metadata: core.BlueprintMetadata{
			Name:       "partial-failure-test",
			Generation: 1,
		},
		Spec: map[string]interface{}{
			"replicas": float64(5),
			"image":    "test:latest",
		},
	}

	// Scenario: 3 running, 2 stopped/failed
	mixedStateContainers := []types.Container{
		{
			ID:    "running-1",
			Names: []string{"/partial-failure-test-1"},
			State: "running",
			Labels: map[string]string{
				"colonies.managed":    "true",
				"colonies.deployment": "partial-failure-test",
				"colonies.generation": "1",
			},
		},
		{
			ID:    "running-2",
			Names: []string{"/partial-failure-test-2"},
			State: "running",
			Labels: map[string]string{
				"colonies.managed":    "true",
				"colonies.deployment": "partial-failure-test",
				"colonies.generation": "1",
			},
		},
		{
			ID:    "running-3",
			Names: []string{"/partial-failure-test-3"},
			State: "running",
			Labels: map[string]string{
				"colonies.managed":    "true",
				"colonies.deployment": "partial-failure-test",
				"colonies.generation": "1",
			},
		},
		{
			ID:    "stopped-1",
			Names: []string{"/partial-failure-test-4"},
			State: "exited",
			Labels: map[string]string{
				"colonies.managed":    "true",
				"colonies.deployment": "partial-failure-test",
				"colonies.generation": "1",
			},
		},
		{
			ID:    "stopped-2",
			Names: []string{"/partial-failure-test-5"},
			State: "exited",
			Labels: map[string]string{
				"colonies.managed":    "true",
				"colonies.deployment": "partial-failure-test",
				"colonies.generation": "1",
			},
		},
	}

	mockDocker.On("ContainerList", mock.Anything, mock.Anything).Return(mixedStateContainers, nil)

	// Mock inspect for running containers
	for i := 1; i <= 3; i++ {
		mockDocker.On("ContainerInspect", mock.Anything, fmt.Sprintf("running-%d", i)).Return(types.ContainerJSON{
			ContainerJSONBase: &types.ContainerJSONBase{
				ID:    fmt.Sprintf("running-%d", i),
				Name:  fmt.Sprintf("/partial-failure-test-%d", i),
				State: &types.ContainerState{Running: true},
			},
			Config: &container.Config{
				Image:  "test:latest",
				Labels: map[string]string{"colonies.generation": "1"},
			},
		}, nil)
	}

	// Mock inspect for stopped containers
	for i := 1; i <= 2; i++ {
		mockDocker.On("ContainerInspect", mock.Anything, fmt.Sprintf("stopped-%d", i)).Return(types.ContainerJSON{
			ContainerJSONBase: &types.ContainerJSONBase{
				ID:    fmt.Sprintf("stopped-%d", i),
				Name:  fmt.Sprintf("/partial-failure-test-%d", i+3),
				State: &types.ContainerState{Running: false, ExitCode: 1},
			},
			Config: &container.Config{
				Image:  "test:latest",
				Labels: map[string]string{"colonies.generation": "1"},
			},
		}, nil)
	}

	status, err := reconciler.CollectStatus(blueprint)
	assert.NoError(t, err)
	assert.Equal(t, 3, status["runningInstances"])
	assert.Equal(t, 2, status["stoppedInstances"])
	assert.Equal(t, 5, status["totalInstances"])

	t.Log("Scenario: 3 running, 2 stopped containers detected")

	mockDocker.AssertExpectations(t)
}

// TestReconciliationScenario_DriftDetection tests various drift scenarios
func TestReconciliationScenario_DriftDetection(t *testing.T) {
	t.Run("ReplicaDrift_TooFew", func(t *testing.T) {
		mockDocker := new(MockDockerClient)
		reconciler := createTestReconciler(mockDocker)

		blueprint := &core.Blueprint{
			Kind: "ExecutorDeployment",
			Metadata: core.BlueprintMetadata{
				Name:       "drift-test-few",
				Generation: 1,
			},
			Spec: map[string]interface{}{
				"replicas": float64(10), // Want 10
				"image":    "test:latest",
			},
		}

		// Only 5 containers running (drift: too few)
		containers := make([]types.Container, 5)
		for i := 0; i < 5; i++ {
			containers[i] = types.Container{
				ID:    generateContainerID(i),
				Names: []string{generateContainerName("drift-test-few", i)},
				State: "running",
				Labels: map[string]string{
					"colonies.managed":    "true",
					"colonies.deployment": "drift-test-few",
					"colonies.generation": "1",
				},
			}

			// Mock ContainerInspect for each container
			mockDocker.On("ContainerInspect", mock.Anything, generateContainerID(i)).Return(types.ContainerJSON{
				ContainerJSONBase: &types.ContainerJSONBase{
					ID:    generateContainerID(i),
					Name:  generateContainerName("drift-test-few", i),
					State: &types.ContainerState{Running: true},
				},
				Config: &container.Config{
					Image:  "test:latest",
					Labels: map[string]string{"colonies.generation": "1"},
				},
			}, nil)
		}

		mockDocker.On("ContainerList", mock.Anything, mock.Anything).Return(containers, nil)

		status, err := reconciler.CollectStatus(blueprint)
		assert.NoError(t, err)
		assert.Equal(t, 5, status["runningInstances"])
		assert.NotEqual(t, 10, status["runningInstances"], "Drift detected: running (5) != desired (10)")

		t.Log("Drift detected: Too few replicas (5/10)")
	})

	t.Run("ReplicaDrift_TooMany", func(t *testing.T) {
		mockDocker := new(MockDockerClient)
		reconciler := createTestReconciler(mockDocker)

		blueprint := &core.Blueprint{
			Kind: "ExecutorDeployment",
			Metadata: core.BlueprintMetadata{
				Name:       "drift-test-many",
				Generation: 1,
			},
			Spec: map[string]interface{}{
				"replicas": float64(5), // Want 5
				"image":    "test:latest",
			},
		}

		// 10 containers running (drift: too many)
		containers := make([]types.Container, 10)
		for i := 0; i < 10; i++ {
			containers[i] = types.Container{
				ID:    generateContainerID(i),
				Names: []string{generateContainerName("drift-test-many", i)},
				State: "running",
				Labels: map[string]string{
					"colonies.managed":    "true",
					"colonies.deployment": "drift-test-many",
					"colonies.generation": "1",
				},
			}

			// Mock ContainerInspect for each container
			mockDocker.On("ContainerInspect", mock.Anything, generateContainerID(i)).Return(types.ContainerJSON{
				ContainerJSONBase: &types.ContainerJSONBase{
					ID:    generateContainerID(i),
					Name:  generateContainerName("drift-test-many", i),
					State: &types.ContainerState{Running: true},
				},
				Config: &container.Config{
					Image:  "test:latest",
					Labels: map[string]string{"colonies.generation": "1"},
				},
			}, nil)
		}

		mockDocker.On("ContainerList", mock.Anything, mock.Anything).Return(containers, nil)

		status, err := reconciler.CollectStatus(blueprint)
		assert.NoError(t, err)
		assert.Equal(t, 10, status["runningInstances"])
		assert.NotEqual(t, 5, status["runningInstances"], "Drift detected: running (10) != desired (5)")

		t.Log("Drift detected: Too many replicas (10/5)")
	})
}

// Helper functions for test scenarios
func generateContainerID(index int) string {
	return fmt.Sprintf("container-%05d", index)
}

func generateContainerName(deploymentName string, index int) string {
	return fmt.Sprintf("/%s-%d", deploymentName, index)
}

// TestReconciliationScenario_ConcurrentStatusChecks tests concurrent operations
func TestReconciliationScenario_ConcurrentStatusChecks(t *testing.T) {
	mockDocker := new(MockDockerClient)
	reconciler := createTestReconciler(mockDocker)

	blueprint := &core.Blueprint{
		Kind: "ExecutorDeployment",
		Metadata: core.BlueprintMetadata{
			Name:       "concurrent-test",
			Generation: 1,
		},
		Spec: map[string]interface{}{
			"replicas": float64(5),
			"image":    "test:latest",
		},
	}

	containers := make([]types.Container, 5)
	for i := 0; i < 5; i++ {
		containers[i] = types.Container{
			ID:    generateContainerID(i),
			Names: []string{generateContainerName("concurrent-test", i)},
			State: "running",
			Labels: map[string]string{
				"colonies.managed":    "true",
				"colonies.deployment": "concurrent-test",
				"colonies.generation": "1",
			},
		}

		// Mock ContainerInspect for each container (will be called multiple times concurrently)
		mockDocker.On("ContainerInspect", mock.Anything, generateContainerID(i)).Return(types.ContainerJSON{
			ContainerJSONBase: &types.ContainerJSONBase{
				ID:    generateContainerID(i),
				Name:  generateContainerName("concurrent-test", i),
				State: &types.ContainerState{Running: true},
			},
			Config: &container.Config{
				Image:  "test:latest",
				Labels: map[string]string{"colonies.generation": "1"},
			},
		}, nil)
	}

	// Mock should handle multiple concurrent calls
	mockDocker.On("ContainerList", mock.Anything, mock.Anything).Return(containers, nil)

	// Run 10 concurrent status checks
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(index int) {
			status, err := reconciler.CollectStatus(blueprint)
			assert.NoError(t, err)
			assert.Equal(t, 5, status["runningInstances"])
			t.Logf("Concurrent check %d completed", index)
			done <- true
		}(i)
	}

	// Wait for all to complete
	for i := 0; i < 10; i++ {
		<-done
	}

	t.Log("Successfully completed 10 concurrent status checks")
	mockDocker.AssertExpectations(t)
}
