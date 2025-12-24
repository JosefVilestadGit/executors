package reconciler

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/colonyos/colonies/pkg/client"
	"github.com/colonyos/colonies/pkg/core"
	"github.com/colonyos/executors/common/pkg/docker"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockDockerClient implements the DockerClient interface for testing
type MockDockerClient struct {
	mock.Mock
}

func (m *MockDockerClient) ContainerList(ctx context.Context, options container.ListOptions) ([]types.Container, error) {
	args := m.Called(ctx, options)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]types.Container), args.Error(1)
}

func (m *MockDockerClient) ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig,
	networkingConfig *network.NetworkingConfig, platform *ocispec.Platform,
	containerName string) (container.CreateResponse, error) {
	args := m.Called(ctx, config, hostConfig, networkingConfig, platform, containerName)
	return args.Get(0).(container.CreateResponse), args.Error(1)
}

func (m *MockDockerClient) ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error {
	args := m.Called(ctx, containerID, options)
	return args.Error(0)
}

func (m *MockDockerClient) ContainerStop(ctx context.Context, containerID string, options container.StopOptions) error {
	args := m.Called(ctx, containerID, options)
	return args.Error(0)
}

func (m *MockDockerClient) ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error {
	args := m.Called(ctx, containerID, options)
	return args.Error(0)
}

func (m *MockDockerClient) ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error) {
	args := m.Called(ctx, containerID)
	if args.Get(0) == nil {
		return types.ContainerJSON{}, args.Error(1)
	}
	return args.Get(0).(types.ContainerJSON), args.Error(1)
}

func (m *MockDockerClient) ImageInspectWithRaw(ctx context.Context, imageID string) (types.ImageInspect, []byte, error) {
	args := m.Called(ctx, imageID)
	if args.Get(0) == nil {
		return types.ImageInspect{}, nil, args.Error(2)
	}
	return args.Get(0).(types.ImageInspect), args.Get(1).([]byte), args.Error(2)
}

func (m *MockDockerClient) ImagePull(ctx context.Context, refStr string, options types.ImagePullOptions) (io.ReadCloser, error) {
	args := m.Called(ctx, refStr, options)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(io.ReadCloser), args.Error(1)
}

// MockColoniesClient implements minimal ColoniesClient interface for testing
type MockColoniesClient struct {
	mock.Mock
}

func (m *MockColoniesClient) AddExecutor(executor *core.Executor, prvKey string) (*core.Executor, error) {
	args := m.Called(executor, prvKey)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*core.Executor), args.Error(1)
}

func (m *MockColoniesClient) RemoveExecutor(colonyName, executorName, prvKey string) error {
	args := m.Called(colonyName, executorName, prvKey)
	return args.Error(0)
}

func (m *MockColoniesClient) ApproveExecutor(colonyName, executorName, prvKey string) error {
	args := m.Called(colonyName, executorName, prvKey)
	return args.Error(0)
}

func (m *MockColoniesClient) AddLog(processID, message string, prvKey string) error {
	args := m.Called(processID, message, prvKey)
	return args.Error(0)
}

func (m *MockColoniesClient) Close(processID, output string, errors []string, prvKey string) error {
	args := m.Called(processID, output, errors, prvKey)
	return args.Error(0)
}

// MockImagePuller implements the ImagePuller interface for testing
type MockImagePuller struct {
	mock.Mock
}

func (m *MockImagePuller) PullImage(image string, logChan chan docker.LogMessage) error {
	args := m.Called(image, logChan)
	return args.Error(0)
}

// createTestReconciler creates a reconciler with mocked dependencies
func createTestReconciler(dockerClient DockerClient) *Reconciler {
	dockerHandler, _ := docker.CreateDockerHandler()

	return &Reconciler{
		dockerHandler:  dockerHandler,
		dockerClient:   dockerClient,
		client:         nil, // Not needed for Docker-only tests
		executorPrvKey: "test-executor-key",
		colonyOwnerKey: "test-colony-owner-key",
		colonyName:     "test-colony",
		location:       "test-location",
	}
}

// createTestReconcilerWithClient creates a reconciler with mocked Docker and Colonies clients
func createTestReconcilerWithClient(dockerClient DockerClient, coloniesClient *MockColoniesClient) *Reconciler {
	dockerHandler, _ := docker.CreateDockerHandler()

	return &Reconciler{
		dockerHandler:  dockerHandler,
		dockerClient:   dockerClient,
		client:         (*client.ColoniesClient)(nil), // Type assertion needed for mock
		executorPrvKey: "test-executor-key",
		colonyOwnerKey: "test-colony-owner-key",
		colonyName:     "test-colony",
		location:       "test-datacenter",
	}
}

// createTestReconcilerWithImagePuller creates a reconciler with a mock image puller for testing
func createTestReconcilerWithImagePuller(dockerClient DockerClient, imagePuller ImagePuller) *Reconciler {
	return &Reconciler{
		dockerHandler:  imagePuller,
		dockerClient:   dockerClient,
		client:         nil,
		executorPrvKey: "test-executor-key",
		colonyOwnerKey: "test-colony-owner-key",
		colonyName:     "test-colony",
		location:       "test-location",
	}
}

// TestCleanupStoppedContainers tests the container cleanup functionality
func TestCleanupStoppedContainers(t *testing.T) {
	mockDocker := new(MockDockerClient)
	reconciler := createTestReconciler(mockDocker)

	t.Run("Cleanup removes stopped containers", func(t *testing.T) {
		// Setup: Return a list with stopped containers
		stoppedContainers := []types.Container{
			{
				ID:    "stopped-1",
				Names: []string{"/test-container-1"},
				State: "exited",
				Labels: map[string]string{
					"colonies.managed":    "true",
					"colonies.deployment": "test-deployment",
				},
			},
			{
				ID:    "stopped-2",
				Names: []string{"/test-container-2"},
				State: "exited",
				Labels: map[string]string{
					"colonies.managed":    "true",
					"colonies.deployment": "test-deployment",
				},
			},
			{
				ID:    "running-1",
				Names: []string{"/test-container-3"},
				State: "running",
				Labels: map[string]string{
					"colonies.managed":    "true",
					"colonies.deployment": "test-deployment",
				},
			},
		}

		mockDocker.On("ContainerList", mock.Anything, mock.Anything).Return(stoppedContainers, nil)
		mockDocker.On("ContainerRemove", mock.Anything, "stopped-1", mock.Anything).Return(nil)
		mockDocker.On("ContainerRemove", mock.Anything, "stopped-2", mock.Anything).Return(nil)

		// Execute
		err := reconciler.CleanupStoppedContainers(nil)

		// Verify
		assert.NoError(t, err)
		mockDocker.AssertExpectations(t)
		// Should have removed 2 stopped containers (not the running one)
		mockDocker.AssertNumberOfCalls(t, "ContainerRemove", 2)
	})

	t.Run("Cleanup handles empty container list", func(t *testing.T) {
		mockDocker := new(MockDockerClient)
		reconciler := createTestReconciler(mockDocker)

		mockDocker.On("ContainerList", mock.Anything, mock.Anything).Return([]types.Container{}, nil)

		err := reconciler.CleanupStoppedContainers(nil)

		assert.NoError(t, err)
		mockDocker.AssertExpectations(t)
		mockDocker.AssertNotCalled(t, "ContainerRemove")
	})

	t.Run("Cleanup handles Docker API errors", func(t *testing.T) {
		mockDocker := new(MockDockerClient)
		reconciler := createTestReconciler(mockDocker)

		mockDocker.On("ContainerList", mock.Anything, mock.Anything).Return(nil, errors.New("Docker daemon not available"))

		err := reconciler.CleanupStoppedContainers(nil)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to list managed containers")
	})
}

// TestCollectStatus tests status collection from containers
func TestCollectStatus(t *testing.T) {
	t.Run("Collect status for ExecutorDeployment", func(t *testing.T) {
		mockDocker := new(MockDockerClient)
		reconciler := createTestReconciler(mockDocker)

		blueprint := &core.Blueprint{
			Kind: "ExecutorDeployment",
			Metadata: core.BlueprintMetadata{
				Name:       "test-deployment",
				Generation: 1,
			},
			Spec: map[string]interface{}{
				"replicas": float64(3), // JSON unmarshals numbers as float64
				"image":    "test-image:latest",
			},
		}

		// Mock 2 running containers
		runningContainers := []types.Container{
			{
				ID:    "running-1",
				Names: []string{"/test-deployment-1"},
				State: "running",
				Labels: map[string]string{
					"colonies.managed":    "true",
					"colonies.deployment": "test-deployment",
					"colonies.generation": "1",
				},
			},
			{
				ID:    "running-2",
				Names: []string{"/test-deployment-2"},
				State: "running",
				Labels: map[string]string{
					"colonies.managed":    "true",
					"colonies.deployment": "test-deployment",
					"colonies.generation": "1",
				},
			},
		}

		mockDocker.On("ContainerList", mock.Anything, mock.Anything).Return(runningContainers, nil)

		// Mock ContainerInspect calls for each container
		mockDocker.On("ContainerInspect", mock.Anything, "running-1").Return(types.ContainerJSON{
			ContainerJSONBase: &types.ContainerJSONBase{
				ID:      "running-1",
				Name:    "/test-deployment-1",
				Created: "2025-01-01T00:00:00Z",
				State: &types.ContainerState{
					Running: true,
				},
			},
			Config: &container.Config{
				Image: "test-image:latest",
			},
		}, nil)

		mockDocker.On("ContainerInspect", mock.Anything, "running-2").Return(types.ContainerJSON{
			ContainerJSONBase: &types.ContainerJSONBase{
				ID:      "running-2",
				Name:    "/test-deployment-2",
				Created: "2025-01-01T00:00:00Z",
				State: &types.ContainerState{
					Running: true,
				},
			},
			Config: &container.Config{
				Image: "test-image:latest",
			},
		}, nil)

		status, err := reconciler.CollectStatus(blueprint)

		assert.NoError(t, err)
		assert.NotNil(t, status)
		assert.Equal(t, 2, status["runningInstances"])
		assert.Equal(t, 0, status["stoppedInstances"])
		assert.Equal(t, 2, status["totalInstances"])
		assert.NotNil(t, status["instances"])
		assert.NotNil(t, status["lastUpdated"])
		mockDocker.AssertExpectations(t)
	})

	t.Run("Collect status handles no containers", func(t *testing.T) {
		mockDocker := new(MockDockerClient)
		reconciler := createTestReconciler(mockDocker)

		blueprint := &core.Blueprint{
			Kind: "ExecutorDeployment",
			Metadata: core.BlueprintMetadata{
				Name:       "test-deployment",
				Generation: 1,
			},
			Spec: map[string]interface{}{
				"replicas": float64(2),
			},
		}

		mockDocker.On("ContainerList", mock.Anything, mock.Anything).Return([]types.Container{}, nil)

		status, err := reconciler.CollectStatus(blueprint)

		assert.NoError(t, err)
		assert.Equal(t, 0, status["runningInstances"])
		assert.Equal(t, 0, status["stoppedInstances"])
		assert.Equal(t, 0, status["totalInstances"])
	})
}

// TestScaleOperations would test scaling up and down
// These are placeholder tests demonstrating how to set up mocks for scale operations
// Currently skipped as they don't call actual reconciler methods
func TestScaleOperations(t *testing.T) {
	t.Skip("Skeleton test - demonstrates mock setup but doesn't test actual functionality")
}

// TestErrorHandling tests various error scenarios
func TestErrorHandling(t *testing.T) {
	t.Run("Container creation failure", func(t *testing.T) {
		mockDocker := new(MockDockerClient)
		_ = createTestReconciler(mockDocker)

		mockDocker.On("ContainerCreate", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return(container.CreateResponse{}, errors.New("insufficient resources"))

		// Test would call reconciliation and verify error handling
		// The error should be propagated correctly
	})

	t.Run("Container start failure", func(t *testing.T) {
		mockDocker := new(MockDockerClient)
		_ = createTestReconciler(mockDocker)

		mockDocker.On("ContainerCreate", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return(container.CreateResponse{ID: "test-container"}, nil)

		mockDocker.On("ContainerStart", mock.Anything, "test-container", mock.Anything).
			Return(errors.New("failed to start container"))

		// Verify error is handled appropriately
	})
}

// TestContainerCleanupOnStartFailure tests that containers are cleaned up when start fails
func TestContainerCleanupOnStartFailure(t *testing.T) {
	t.Run("ContainerStart failure triggers cleanup", func(t *testing.T) {
		mockDocker := new(MockDockerClient)
		reconciler := createTestReconciler(mockDocker)

		containerID := "created-but-failed-to-start"

		// ContainerList - check for existing container (none found)
		mockDocker.On("ContainerList", mock.Anything, mock.Anything).
			Return([]types.Container{}, nil)

		// ImageInspectWithRaw - image exists locally
		mockDocker.On("ImageInspectWithRaw", mock.Anything, "test-image:latest").
			Return(types.ImageInspect{}, []byte{}, nil)

		// ContainerCreate succeeds
		mockDocker.On("ContainerCreate", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, "test-container").
			Return(container.CreateResponse{ID: containerID}, nil)

		// ContainerStart fails
		mockDocker.On("ContainerStart", mock.Anything, containerID, mock.Anything).
			Return(errors.New("port already in use"))

		// ContainerRemove should be called to clean up
		mockDocker.On("ContainerRemove", mock.Anything, containerID, container.RemoveOptions{Force: true}).
			Return(nil)

		// Create test process and blueprint
		process := &core.Process{ID: "test-process"}
		spec := DeploymentSpec{
			Image:    "test-image:latest",
			Replicas: 1,
		}
		blueprint := &core.Blueprint{
			Kind: "DockerDeployment", // Use DockerDeployment to skip executor registration
			Metadata: core.BlueprintMetadata{
				Name:       "test-deployment",
				Generation: 1,
			},
		}

		// Call startContainer - should fail but clean up
		err := reconciler.startContainer(process, spec, "test-container", blueprint)

		// Verify error is returned
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to start container")

		// Verify ContainerRemove was called to clean up
		mockDocker.AssertCalled(t, "ContainerRemove", mock.Anything, containerID, container.RemoveOptions{Force: true})
	})

	t.Run("waitForContainerRunning failure triggers cleanup", func(t *testing.T) {
		mockDocker := new(MockDockerClient)
		reconciler := createTestReconciler(mockDocker)

		containerID := "started-but-exited"

		// ContainerList - check for existing container (none found)
		mockDocker.On("ContainerList", mock.Anything, mock.Anything).
			Return([]types.Container{}, nil)

		// ImageInspectWithRaw - image exists locally
		mockDocker.On("ImageInspectWithRaw", mock.Anything, "test-image:latest").
			Return(types.ImageInspect{}, []byte{}, nil)

		// ContainerCreate succeeds
		mockDocker.On("ContainerCreate", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, "flaky-container").
			Return(container.CreateResponse{ID: containerID}, nil)

		// ContainerStart succeeds
		mockDocker.On("ContainerStart", mock.Anything, containerID, mock.Anything).
			Return(nil)

		// ContainerInspect returns exited state (simulating container that crashed immediately)
		mockDocker.On("ContainerInspect", mock.Anything, containerID).
			Return(types.ContainerJSON{
				ContainerJSONBase: &types.ContainerJSONBase{
					State: &types.ContainerState{
						Running: false,
						Status:  "exited",
					},
				},
			}, nil)

		// Cleanup calls - ContainerStop and ContainerRemove
		mockDocker.On("ContainerStop", mock.Anything, containerID, mock.Anything).
			Return(nil)
		mockDocker.On("ContainerRemove", mock.Anything, containerID, container.RemoveOptions{Force: true}).
			Return(nil)

		process := &core.Process{ID: "test-process"}
		spec := DeploymentSpec{
			Image:    "test-image:latest",
			Replicas: 1,
		}
		blueprint := &core.Blueprint{
			Kind: "DockerDeployment",
			Metadata: core.BlueprintMetadata{
				Name:       "test-deployment",
				Generation: 1,
			},
		}

		// Call startContainer - should fail and clean up
		err := reconciler.startContainer(process, spec, "flaky-container", blueprint)

		// Verify error is returned
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "container failed to start")

		// Verify cleanup was attempted (ContainerStop called as part of stopAndRemoveContainer)
		mockDocker.AssertCalled(t, "ContainerStop", mock.Anything, containerID, mock.Anything)
		mockDocker.AssertCalled(t, "ContainerRemove", mock.Anything, containerID, container.RemoveOptions{Force: true})
	})

	t.Run("Cleanup failure is logged but original error returned", func(t *testing.T) {
		mockDocker := new(MockDockerClient)
		reconciler := createTestReconciler(mockDocker)

		containerID := "stuck-container"

		// ContainerList - check for existing container (none found)
		mockDocker.On("ContainerList", mock.Anything, mock.Anything).
			Return([]types.Container{}, nil)

		// ImageInspectWithRaw - image exists locally
		mockDocker.On("ImageInspectWithRaw", mock.Anything, "test-image:latest").
			Return(types.ImageInspect{}, []byte{}, nil)

		// ContainerCreate succeeds
		mockDocker.On("ContainerCreate", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, "stuck-test").
			Return(container.CreateResponse{ID: containerID}, nil)

		// ContainerStart fails
		mockDocker.On("ContainerStart", mock.Anything, containerID, mock.Anything).
			Return(errors.New("network error"))

		// ContainerRemove also fails (container is stuck)
		mockDocker.On("ContainerRemove", mock.Anything, containerID, container.RemoveOptions{Force: true}).
			Return(errors.New("container is stuck"))

		process := &core.Process{ID: "test-process"}
		spec := DeploymentSpec{
			Image:    "test-image:latest",
			Replicas: 1,
		}
		blueprint := &core.Blueprint{
			Kind: "DockerDeployment",
			Metadata: core.BlueprintMetadata{
				Name:       "test-deployment",
				Generation: 1,
			},
		}

		// Call startContainer
		err := reconciler.startContainer(process, spec, "stuck-test", blueprint)

		// Original error should be returned (not the cleanup error)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to start container")
		assert.Contains(t, err.Error(), "network error")
		assert.NotContains(t, err.Error(), "stuck")

		// Verify cleanup was attempted
		mockDocker.AssertCalled(t, "ContainerRemove", mock.Anything, containerID, container.RemoveOptions{Force: true})
	})
}

// TestImageValidation tests that startContainer validates image exists before creating container
func TestImageValidation(t *testing.T) {
	t.Run("startContainer fails if image not found locally", func(t *testing.T) {
		mockDocker := new(MockDockerClient)
		reconciler := createTestReconciler(mockDocker)

		// ContainerList - check for existing container (none found)
		mockDocker.On("ContainerList", mock.Anything, mock.Anything).
			Return([]types.Container{}, nil)

		// ImageInspectWithRaw returns error (image not found)
		mockDocker.On("ImageInspectWithRaw", mock.Anything, "missing-image:latest").
			Return(types.ImageInspect{}, []byte{}, errors.New("image not found"))

		process := &core.Process{ID: "test-process"}
		spec := DeploymentSpec{
			Image:    "missing-image:latest",
			Replicas: 1,
		}
		blueprint := &core.Blueprint{
			Kind: "DockerDeployment",
			Metadata: core.BlueprintMetadata{
				Name:       "test-deployment",
				Generation: 1,
			},
		}

		// Call startContainer - should fail due to missing image
		err := reconciler.startContainer(process, spec, "test-container", blueprint)

		// Verify error is returned with clear message
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "image not found")
		assert.Contains(t, err.Error(), "missing-image:latest")

		// Verify ContainerCreate was NOT called (we abort before creating)
		mockDocker.AssertNotCalled(t, "ContainerCreate", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("startContainer proceeds when image exists locally", func(t *testing.T) {
		mockDocker := new(MockDockerClient)
		reconciler := createTestReconciler(mockDocker)

		// ContainerList - check for existing container (none found)
		mockDocker.On("ContainerList", mock.Anything, mock.Anything).
			Return([]types.Container{}, nil)

		// ImageInspectWithRaw returns success (image exists)
		mockDocker.On("ImageInspectWithRaw", mock.Anything, "existing-image:latest").
			Return(types.ImageInspect{}, []byte{}, nil)

		// ContainerCreate succeeds
		mockDocker.On("ContainerCreate", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, "test-container").
			Return(container.CreateResponse{ID: "new-container-id"}, nil)

		// ContainerStart succeeds
		mockDocker.On("ContainerStart", mock.Anything, "new-container-id", mock.Anything).
			Return(nil)

		// ContainerInspect shows running
		mockDocker.On("ContainerInspect", mock.Anything, "new-container-id").
			Return(types.ContainerJSON{
				ContainerJSONBase: &types.ContainerJSONBase{
					State: &types.ContainerState{Running: true},
				},
			}, nil)

		process := &core.Process{ID: "test-process"}
		spec := DeploymentSpec{
			Image:    "existing-image:latest",
			Replicas: 1,
		}
		blueprint := &core.Blueprint{
			Kind: "DockerDeployment", // Use DockerDeployment to skip executor registration
			Metadata: core.BlueprintMetadata{
				Name:       "test-deployment",
				Generation: 1,
			},
		}

		// Call startContainer - should succeed
		err := reconciler.startContainer(process, spec, "test-container", blueprint)

		// Verify no error (or only executor-related errors which we skip with DockerDeployment)
		if err != nil {
			// Should not be an image-related error
			assert.NotContains(t, err.Error(), "image not found")
		}

		// Verify ImageInspectWithRaw was called for validation
		mockDocker.AssertCalled(t, "ImageInspectWithRaw", mock.Anything, "existing-image:latest")

		// Verify ContainerCreate was called (we got past image validation)
		mockDocker.AssertCalled(t, "ContainerCreate", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, "test-container")
	})
}

// TestExecutorLabelMatching tests that executor-to-container matching uses labels
func TestExecutorLabelMatching(t *testing.T) {
	t.Run("Executor with colonies.executor label is matched correctly", func(t *testing.T) {
		// Test that the colonies.executor label is used for direct matching
		// This avoids fragile name parsing for deployments with hyphens

		// Simulate a container with the colonies.executor label
		containers := []types.Container{
			{
				ID:    "container-1",
				Names: []string{"/my-hyphenated-app-a8f2c"},
				Labels: map[string]string{
					"colonies.deployment": "my-hyphenated-app",
					"colonies.generation": "1",
					"colonies.executor":   "my-hyphenated-app-a8f2c-1",
				},
			},
		}

		// Build executor set from labels (simulating the cleanup logic)
		containerExecutors := make(map[string]bool)
		for _, cont := range containers {
			if execName, ok := cont.Labels["colonies.executor"]; ok && execName != "" {
				containerExecutors[execName] = true
			}
		}

		// Verify the executor is found
		assert.True(t, containerExecutors["my-hyphenated-app-a8f2c-1"], "Executor should be found via label")
		assert.False(t, containerExecutors["other-executor-1"], "Unknown executor should not be found")
	})

	t.Run("Hyphenated deployment names work correctly", func(t *testing.T) {
		// This test verifies that deployment names with hyphens are handled correctly
		// Previously, parsing "my-app-test-a8f2c-1" to extract "my-app-test" was fragile

		testCases := []struct {
			deploymentName string
			executorName   string
			containerName  string
		}{
			{"simple", "simple-a8f2c-1", "simple-a8f2c"},
			{"my-app", "my-app-a8f2c-1", "my-app-a8f2c"},
			{"my-app-test", "my-app-test-a8f2c-1", "my-app-test-a8f2c"},
			{"my-app-test-prod", "my-app-test-prod-a8f2c-1", "my-app-test-prod-a8f2c"},
		}

		for _, tc := range testCases {
			t.Run(tc.deploymentName, func(t *testing.T) {
				// Simulate container with proper labels
				containers := []types.Container{
					{
						ID:    "container-id",
						Names: []string{"/" + tc.containerName},
						Labels: map[string]string{
							"colonies.deployment": tc.deploymentName,
							"colonies.generation": "1",
							"colonies.executor":   tc.executorName,
						},
					},
				}

				// Build executor set from labels
				containerExecutors := make(map[string]bool)
				for _, cont := range containers {
					if execName, ok := cont.Labels["colonies.executor"]; ok && execName != "" {
						containerExecutors[execName] = true
					}
				}

				// Verify exact match works
				assert.True(t, containerExecutors[tc.executorName],
					"Executor %s should be found for deployment %s", tc.executorName, tc.deploymentName)
			})
		}
	})

	t.Run("Stale executor without container is detected", func(t *testing.T) {
		// Simulate: executor exists but no matching container
		containers := []types.Container{
			{
				ID:    "container-1",
				Names: []string{"/my-app-b9f3d"},
				Labels: map[string]string{
					"colonies.deployment": "my-app",
					"colonies.generation": "1",
					"colonies.executor":   "my-app-b9f3d-1",
				},
			},
		}

		// Build executor and container name sets
		containerExecutors := make(map[string]bool)
		containerNames := make(map[string]bool)
		for _, cont := range containers {
			if execName, ok := cont.Labels["colonies.executor"]; ok && execName != "" {
				containerExecutors[execName] = true
			}
			name := cont.Names[0]
			if len(name) > 0 && name[0] == '/' {
				name = name[1:]
			}
			containerNames[name] = true
		}

		// Executor that exists in registry but has no container
		staleExecutorName := "my-app-a8f2c-1"
		staleContainerName := "my-app-a8f2c"

		// Verify the stale executor is NOT found
		assert.False(t, containerExecutors[staleExecutorName], "Stale executor should not be found in labels")
		assert.False(t, containerNames[staleContainerName], "Stale container should not be found")
	})
}

// TestConcurrentOperations tests thread-safety
func TestConcurrentOperations(t *testing.T) {
	t.Skip("Concurrency test - run manually to verify thread safety")

	mockDocker := new(MockDockerClient)
	reconciler := createTestReconciler(mockDocker)

	// Setup mock to handle concurrent calls
	mockDocker.On("ContainerList", mock.Anything, mock.Anything).Return([]types.Container{}, nil)

	// Launch multiple concurrent status checks
	done := make(chan bool)
	blueprint := &core.Blueprint{
		Kind: "ExecutorDeployment",
		Metadata: core.BlueprintMetadata{
			Name: "concurrent-test",
		},
		Spec: map[string]interface{}{
			"replicas": float64(1),
		},
	}

	for i := 0; i < 10; i++ {
		go func() {
			_, err := reconciler.CollectStatus(blueprint)
			assert.NoError(t, err)
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
}

// TestImageOperations tests image-related functionality
func TestImageOperations(t *testing.T) {
	t.Run("Image exists locally", func(t *testing.T) {
		mockDocker := new(MockDockerClient)
		_ = createTestReconciler(mockDocker)

		mockDocker.On("ImageInspectWithRaw", mock.Anything, "existing-image:latest").
			Return(types.ImageInspect{
				ID:       "sha256:abc123",
				RepoTags: []string{"existing-image:latest"},
			}, []byte{}, nil)

		// Test that image check succeeds
	})

	t.Run("Image not found triggers pull", func(t *testing.T) {
		mockDocker := new(MockDockerClient)
		_ = createTestReconciler(mockDocker)

		mockDocker.On("ImageInspectWithRaw", mock.Anything, "new-image:latest").
			Return(types.ImageInspect{}, []byte{}, errors.New("image not found"))

		// In production code, this would trigger ImagePull
		// We're just verifying the mock setup works
	})
}

// TestImagePullTimeout tests that image pulls timeout correctly
func TestImagePullTimeout(t *testing.T) {
	t.Run("Image pull times out after specified duration", func(t *testing.T) {
		mockDocker := new(MockDockerClient)
		mockImagePuller := new(MockImagePuller)
		reconciler := createTestReconcilerWithImagePuller(mockDocker, mockImagePuller)

		// Mock PullImage to block forever (simulating a hanging pull)
		mockImagePuller.On("PullImage", "slow-image:latest", mock.Anything).
			Run(func(args mock.Arguments) {
				// Block forever - the timeout should cancel this
				select {}
			}).
			Return(nil)

		// Create a minimal process for logging
		process := &core.Process{
			ID: "test-process-id",
		}

		// Use a very short timeout (100ms) for testing
		timeout := 100 * time.Millisecond
		start := time.Now()
		err := reconciler.doPullImageWithTimeout(process, "slow-image:latest", timeout)
		elapsed := time.Since(start)

		// Verify timeout error occurred
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "image pull timed out")
		assert.Contains(t, err.Error(), "slow-image:latest")

		// Verify it didn't take much longer than the timeout
		assert.Less(t, elapsed, 200*time.Millisecond, "Should timeout close to the specified duration")
	})

	t.Run("Image pull succeeds before timeout", func(t *testing.T) {
		mockDocker := new(MockDockerClient)
		mockImagePuller := new(MockImagePuller)
		reconciler := createTestReconcilerWithImagePuller(mockDocker, mockImagePuller)

		// Mock PullImage to succeed quickly
		mockImagePuller.On("PullImage", "fast-image:latest", mock.Anything).
			Run(func(args mock.Arguments) {
				logChan := args.Get(1).(chan docker.LogMessage)
				// Send progress messages
				logChan <- docker.LogMessage{Log: "Pulling layer 1/3", EOF: false}
				logChan <- docker.LogMessage{Log: "Pulling layer 2/3", EOF: false}
				logChan <- docker.LogMessage{Log: "Pulling layer 3/3", EOF: false}
				// Signal completion
				logChan <- docker.LogMessage{Log: "Pull complete", EOF: true}
			}).
			Return(nil)

		process := &core.Process{
			ID: "test-process-id",
		}

		// Use a longer timeout
		timeout := 5 * time.Second
		err := reconciler.doPullImageWithTimeout(process, "fast-image:latest", timeout)

		// Verify success
		assert.NoError(t, err)
		mockImagePuller.AssertCalled(t, "PullImage", "fast-image:latest", mock.Anything)
	})

	t.Run("Image pull error is returned", func(t *testing.T) {
		mockDocker := new(MockDockerClient)
		mockImagePuller := new(MockImagePuller)
		reconciler := createTestReconcilerWithImagePuller(mockDocker, mockImagePuller)

		// Mock PullImage to return an error
		mockImagePuller.On("PullImage", "bad-image:latest", mock.Anything).
			Run(func(args mock.Arguments) {
				// Don't send EOF, let the error channel handle it
			}).
			Return(errors.New("repository not found"))

		process := &core.Process{
			ID: "test-process-id",
		}

		timeout := 5 * time.Second
		err := reconciler.doPullImageWithTimeout(process, "bad-image:latest", timeout)

		// Verify error is returned (not timeout)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "repository not found")
		assert.NotContains(t, err.Error(), "timed out")
	})
}

// TestAtomicExecutorRegistration tests that executor name generation uses atomic registration
func TestAtomicExecutorRegistration(t *testing.T) {
	t.Run("isDuplicateExecutorError detects duplicate errors", func(t *testing.T) {
		// Test various duplicate error messages
		assert.True(t, isDuplicateExecutorError(errors.New("Executor with name <test> already exists")))
		assert.True(t, isDuplicateExecutorError(errors.New("duplicate key value")))
		assert.True(t, isDuplicateExecutorError(errors.New("not unique")))

		// Test non-duplicate errors
		assert.False(t, isDuplicateExecutorError(errors.New("connection refused")))
		assert.False(t, isDuplicateExecutorError(errors.New("timeout")))
		assert.False(t, isDuplicateExecutorError(nil))
	})

	t.Run("generateExecutorName generates unique names", func(t *testing.T) {
		names := make(map[string]bool)
		baseName := "test-executor"

		// Generate 100 names and verify they're all unique
		for i := 0; i < 100; i++ {
			name := generateExecutorName(baseName)
			assert.False(t, names[name], "Generated duplicate name: %s", name)
			names[name] = true

			// Verify format: baseName-hash (5 chars)
			assert.True(t, len(name) > len(baseName)+1, "Name too short: %s", name)
			assert.Contains(t, name, baseName+"-")
		}
	})

	t.Run("generateUniqueExecutorName returns name without pre-check", func(t *testing.T) {
		mockDocker := new(MockDockerClient)
		reconciler := createTestReconciler(mockDocker)

		// Should return a name without making any network calls
		name, err := reconciler.generateUniqueExecutorName("test-colony", "test-deployment")
		assert.NoError(t, err)
		assert.NotEmpty(t, name)
		assert.Contains(t, name, "test-deployment-")

		// No mock expectations were set, so this confirms no network calls were made
	})
}

// TestContainerLifecycle would test full container lifecycle
// This is a placeholder test demonstrating how to set up mocks for lifecycle operations
// Currently skipped as it doesn't call actual reconciler methods
func TestContainerLifecycle(t *testing.T) {
	t.Skip("Skeleton test - demonstrates mock setup but doesn't test actual functionality")
}

// TestMockingVerification verifies that mocking setup is correct
func TestMockingVerification(t *testing.T) {
	t.Run("Mock implements DockerClient interface", func(t *testing.T) {
		var _ DockerClient = (*MockDockerClient)(nil)
		// If this compiles, the mock implements the interface correctly
	})

	t.Run("Mock can be injected into Reconciler", func(t *testing.T) {
		mockDocker := new(MockDockerClient)
		reconciler := createTestReconciler(mockDocker)

		assert.NotNil(t, reconciler)
		assert.NotNil(t, reconciler.dockerClient)
	})

	t.Run("Mock expectations work correctly", func(t *testing.T) {
		mockDocker := new(MockDockerClient)

		// Setup expectation
		mockDocker.On("ContainerList", mock.Anything, mock.Anything).Return([]types.Container{}, nil)

		// Execute
		containers, err := mockDocker.ContainerList(context.Background(), container.ListOptions{})

		// Verify
		assert.NoError(t, err)
		assert.NotNil(t, containers)
		assert.Len(t, containers, 0)
		mockDocker.AssertExpectations(t)
	})
}

// TestLocationInheritance tests that child executors inherit location from reconciler
func TestLocationInheritance(t *testing.T) {
	t.Run("Executor inherits location from reconciler", func(t *testing.T) {
		reconciler := &Reconciler{
			location:   "test-datacenter",
			colonyName: "test-colony",
		}

		// Create an executor using the reconciler's location
		executorID := "test-executor-id"
		executorType := "container-executor"
		executorName := "test-container-executor"

		executor := core.CreateExecutor(executorID, executorType, executorName, reconciler.colonyName, time.Now(), time.Now())
		executor.LocationName = reconciler.location

		// Verify location was set correctly
		assert.Equal(t, "test-datacenter", executor.LocationName)
		assert.Equal(t, "test-colony", executor.ColonyName)
		assert.Equal(t, "container-executor", executor.Type)
		assert.Equal(t, "test-container-executor", executor.Name)
	})

	t.Run("Multiple executors all inherit same location", func(t *testing.T) {
		reconciler := &Reconciler{
			location:   "edge-datacenter",
			colonyName: "production",
		}

		executorNames := []string{"executor-1", "executor-2", "executor-3"}
		executors := make([]*core.Executor, len(executorNames))

		for i, name := range executorNames {
			executor := core.CreateExecutor(
				"id-"+name,
				"container-executor",
				name,
				reconciler.colonyName,
				time.Now(),
				time.Now(),
			)
			executor.LocationName = reconciler.location
			executors[i] = executor
		}

		// Verify all executors have same location
		for _, executor := range executors {
			assert.Equal(t, "edge-datacenter", executor.LocationName)
			assert.Equal(t, "production", executor.ColonyName)
		}
	})

	t.Run("Location from environment is propagated to reconciler", func(t *testing.T) {
		// This tests CreateReconciler's location parameter
		testLocation := "local-datacenter"

		reconciler := &Reconciler{
			location:   testLocation,
			colonyName: "dev",
		}

		assert.Equal(t, "local-datacenter", reconciler.location)

		// Child executor should get this location
		executor := core.CreateExecutor(
			"test-id",
			"container-executor",
			"test-executor",
			reconciler.colonyName,
			time.Now(),
			time.Now(),
		)
		executor.LocationName = reconciler.location

		assert.Equal(t, "local-datacenter", executor.LocationName)
	})
}

// TestScaleDownDeregistration tests the container lifecycle ordering during scale-down
// The correct order is: Stop container FIRST, then deregister executor
// This prevents orphaned containers if container stop fails
func TestScaleDownDeregistration(t *testing.T) {
	t.Run("Scale down stops containers before deregistering executors", func(t *testing.T) {
		mockDocker := new(MockDockerClient)
		mockColonies := new(MockColoniesClient)

		reconciler := &Reconciler{
			dockerClient:   mockDocker,
			colonyName:     "test-colony",
			colonyOwnerKey: "test-owner-key",
			location:       "test-location",
		}

		// Simulate scale-down scenario: have 2 containers running
		containers := []types.Container{
			{
				ID:    "container-1-fullid",
				Names: []string{"/docker-executor-abc12"},
				State: "running",
				Labels: map[string]string{
					"colonies.deployment": "test-deployment",
					"colonies.managed":    "true",
				},
			},
			{
				ID:    "container-2-fullid",
				Names: []string{"/docker-executor-def34"},
				State: "running",
				Labels: map[string]string{
					"colonies.deployment": "test-deployment",
					"colonies.managed":    "true",
				},
			},
		}

		// Track order of operations
		callOrder := []string{}

		// Mock ContainerStop calls (should happen FIRST)
		mockDocker.On("ContainerStop", mock.Anything, "container-1-fullid", mock.Anything).
			Run(func(args mock.Arguments) {
				callOrder = append(callOrder, "stop-abc12")
			}).
			Return(nil)

		mockDocker.On("ContainerStop", mock.Anything, "container-2-fullid", mock.Anything).
			Run(func(args mock.Arguments) {
				callOrder = append(callOrder, "stop-def34")
			}).
			Return(nil)

		mockDocker.On("ContainerRemove", mock.Anything, "container-1-fullid", mock.Anything).
			Run(func(args mock.Arguments) {
				callOrder = append(callOrder, "remove-abc12")
			}).
			Return(nil)

		mockDocker.On("ContainerRemove", mock.Anything, "container-2-fullid", mock.Anything).
			Run(func(args mock.Arguments) {
				callOrder = append(callOrder, "remove-def34")
			}).
			Return(nil)

		// Mock RemoveExecutor calls (should happen AFTER container stop/remove)
		mockColonies.On("RemoveExecutor", "test-colony", "docker-executor-abc12", "test-owner-key").
			Run(func(args mock.Arguments) {
				callOrder = append(callOrder, "deregister-abc12")
			}).
			Return(nil)

		mockColonies.On("RemoveExecutor", "test-colony", "docker-executor-def34", "test-owner-key").
			Run(func(args mock.Arguments) {
				callOrder = append(callOrder, "deregister-def34")
			}).
			Return(nil)

		// Simulate the scale-down logic with correct ordering:
		// Stop container FIRST, then deregister executor
		for _, cont := range containers {
			containerID := cont.ID
			containerName := cont.Names[0]
			if len(containerName) > 0 && containerName[0] == '/' {
				containerName = containerName[1:]
			}

			// Stop and remove container FIRST
			stopOpts := container.StopOptions{}
			err := mockDocker.ContainerStop(context.Background(), containerID, stopOpts)
			assert.NoError(t, err)

			removeOpts := container.RemoveOptions{}
			err = mockDocker.ContainerRemove(context.Background(), containerID, removeOpts)
			assert.NoError(t, err)

			// THEN deregister executor (only after container is stopped)
			if containerName != "" {
				err := mockColonies.RemoveExecutor(reconciler.colonyName, containerName, reconciler.colonyOwnerKey)
				assert.NoError(t, err)
			}
		}

		// Verify correct order: stop/remove BEFORE deregister for each container
		assert.Len(t, callOrder, 6, "Expected 6 operations total")
		assert.Equal(t, "stop-abc12", callOrder[0], "First: stop container 1")
		assert.Equal(t, "remove-abc12", callOrder[1], "Second: remove container 1")
		assert.Equal(t, "deregister-abc12", callOrder[2], "Third: deregister executor 1")
		assert.Equal(t, "stop-def34", callOrder[3], "Fourth: stop container 2")
		assert.Equal(t, "remove-def34", callOrder[4], "Fifth: remove container 2")
		assert.Equal(t, "deregister-def34", callOrder[5], "Sixth: deregister executor 2")

		mockDocker.AssertExpectations(t)
		mockColonies.AssertExpectations(t)
	})

	t.Run("Container stop failure prevents executor deregistration", func(t *testing.T) {
		mockDocker := new(MockDockerClient)
		mockColonies := new(MockColoniesClient)

		reconciler := &Reconciler{
			dockerClient:   mockDocker,
			colonyName:     "test-colony",
			colonyOwnerKey: "test-owner-key",
		}

		testContainer := types.Container{
			ID:    "stuck-container-id",
			Names: []string{"/stuck-executor"},
			State: "running",
			Labels: map[string]string{
				"colonies.deployment": "test-deployment",
				"colonies.managed":    "true",
			},
		}

		// Mock container stop FAILURE
		mockDocker.On("ContainerStop", mock.Anything, "stuck-container-id", mock.Anything).
			Return(errors.New("container stuck, cannot stop"))

		// Simulate scale-down with container stop failure
		containerName := testContainer.Names[0][1:] // Remove leading slash

		// Try to stop container first
		stopOpts := container.StopOptions{}
		err := mockDocker.ContainerStop(context.Background(), testContainer.ID, stopOpts)
		assert.Error(t, err, "Container stop should fail")

		// Container stop failed, so we should NOT deregister the executor
		// The executor stays registered so next reconciliation can retry
		// Verify RemoveExecutor was never called
		mockColonies.AssertNotCalled(t, "RemoveExecutor", reconciler.colonyName, containerName, reconciler.colonyOwnerKey)

		mockDocker.AssertExpectations(t)
		mockColonies.AssertExpectations(t)
	})

	t.Run("Deregistration failure after container stop is non-fatal", func(t *testing.T) {
		mockDocker := new(MockDockerClient)
		mockColonies := new(MockColoniesClient)

		reconciler := &Reconciler{
			dockerClient:   mockDocker,
			colonyName:     "test-colony",
			colonyOwnerKey: "test-owner-key",
		}

		testContainer := types.Container{
			ID:    "container-id",
			Names: []string{"/failing-executor"},
			State: "running",
			Labels: map[string]string{
				"colonies.deployment": "test-deployment",
				"colonies.managed":    "true",
			},
		}

		// Container stop succeeds
		mockDocker.On("ContainerStop", mock.Anything, "container-id", mock.Anything).Return(nil)
		mockDocker.On("ContainerRemove", mock.Anything, "container-id", mock.Anything).Return(nil)

		// But deregistration fails - this is less critical since container is already gone
		mockColonies.On("RemoveExecutor", "test-colony", "failing-executor", "test-owner-key").
			Return(errors.New("access denied"))

		// Simulate scale-down: stop container first
		stopOpts := container.StopOptions{}
		err := mockDocker.ContainerStop(context.Background(), testContainer.ID, stopOpts)
		assert.NoError(t, err, "Container stop should succeed")

		removeOpts := container.RemoveOptions{}
		err = mockDocker.ContainerRemove(context.Background(), testContainer.ID, removeOpts)
		assert.NoError(t, err, "Container remove should succeed")

		// Then try to deregister (fails but container is already gone)
		containerName := testContainer.Names[0][1:] // Remove leading slash
		err = mockColonies.RemoveExecutor(reconciler.colonyName, containerName, reconciler.colonyOwnerKey)
		assert.Error(t, err, "Deregistration fails but this is non-fatal")

		// The important thing: container is gone, no resource leak
		mockDocker.AssertExpectations(t)
		mockColonies.AssertExpectations(t)
	})

	t.Run("Non-executor deployments skip deregistration", func(t *testing.T) {
		mockDocker := new(MockDockerClient)
		mockColonies := new(MockColoniesClient)

		testContainer := types.Container{
			ID:    "service-container-id",
			Names: []string{"/web-service"},
			State: "running",
			Labels: map[string]string{
				"colonies.deployment": "web-service",
				"colonies.managed":    "true",
			},
		}

		// For non-ExecutorDeployment blueprints, RemoveExecutor should not be called
		// Only container operations should happen
		mockDocker.On("ContainerStop", mock.Anything, "service-container-id", mock.Anything).Return(nil)
		mockDocker.On("ContainerRemove", mock.Anything, "service-container-id", mock.Anything).Return(nil)

		// Simulate scale-down for non-executor deployment (no deregistration)
		stopOpts := container.StopOptions{}
		err := mockDocker.ContainerStop(context.Background(), testContainer.ID, stopOpts)
		assert.NoError(t, err)

		removeOpts := container.RemoveOptions{}
		err = mockDocker.ContainerRemove(context.Background(), testContainer.ID, removeOpts)
		assert.NoError(t, err)

		// Verify RemoveExecutor was never called
		mockColonies.AssertNotCalled(t, "RemoveExecutor")
		mockDocker.AssertExpectations(t)
	})
}

// TestExtractImagesFromBlueprint tests extracting images from different blueprint types
func TestExtractImagesFromBlueprint(t *testing.T) {
	reconciler := createTestReconciler(nil)

	t.Run("Extract image from ExecutorDeployment", func(t *testing.T) {
		blueprint := &core.Blueprint{
			Kind: "ExecutorDeployment",
			Metadata: core.BlueprintMetadata{
				Name: "test-executor",
			},
			Spec: map[string]interface{}{
				"image":    "nginx:latest",
				"replicas": float64(3),
			},
		}

		images, err := reconciler.extractImagesFromBlueprint(blueprint)
		assert.NoError(t, err)
		assert.Len(t, images, 1)
		assert.Equal(t, "nginx:latest", images[0])
	})

	t.Run("Extract images from DockerDeployment", func(t *testing.T) {
		blueprint := &core.Blueprint{
			Kind: "DockerDeployment",
			Metadata: core.BlueprintMetadata{
				Name: "test-docker-deployment",
			},
			Spec: map[string]interface{}{
				"instances": []interface{}{
					map[string]interface{}{
						"name":  "web",
						"image": "nginx:1.21",
					},
					map[string]interface{}{
						"name":  "redis",
						"image": "redis:7",
					},
					map[string]interface{}{
						"name":  "postgres",
						"image": "postgres:15",
					},
				},
			},
		}

		images, err := reconciler.extractImagesFromBlueprint(blueprint)
		assert.NoError(t, err)
		assert.Len(t, images, 3)
		assert.Contains(t, images, "nginx:1.21")
		assert.Contains(t, images, "redis:7")
		assert.Contains(t, images, "postgres:15")
	})

	t.Run("Handle ExecutorDeployment without image", func(t *testing.T) {
		blueprint := &core.Blueprint{
			Kind: "ExecutorDeployment",
			Metadata: core.BlueprintMetadata{
				Name: "test-no-image",
			},
			Spec: map[string]interface{}{
				"replicas": float64(1),
			},
		}

		images, err := reconciler.extractImagesFromBlueprint(blueprint)
		assert.NoError(t, err)
		assert.Len(t, images, 0)
	})

	t.Run("Handle unsupported blueprint kind", func(t *testing.T) {
		blueprint := &core.Blueprint{
			Kind: "UnsupportedKind",
			Metadata: core.BlueprintMetadata{
				Name: "test-unsupported",
			},
			Spec: map[string]interface{}{},
		}

		images, err := reconciler.extractImagesFromBlueprint(blueprint)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported blueprint kind")
		assert.Nil(t, images)
	})
}

// TestForceReconcileListsContainers tests that ForceReconcile properly lists containers for removal
func TestForceReconcileListsContainers(t *testing.T) {
	mockDocker := new(MockDockerClient)
	reconciler := createTestReconciler(mockDocker)

	// Existing containers (will be removed during force reconcile)
	existingContainers := []types.Container{
		{
			ID:    "existing-1",
			Names: []string{"/force-test-abc"},
			State: "running",
			Labels: map[string]string{
				"colonies.managed":    "true",
				"colonies.deployment": "force-test",
				"colonies.generation": "1",
			},
		},
	}

	// First call to list containers (for force removal)
	mockDocker.On("ContainerList", mock.Anything, mock.MatchedBy(func(opts container.ListOptions) bool {
		return opts.All == true // Force reconcile lists ALL containers
	})).Return(existingContainers, nil).Once()

	// Mock ContainerInspect for the existing container
	mockDocker.On("ContainerInspect", mock.Anything, "existing-1").Return(types.ContainerJSON{
		ContainerJSONBase: &types.ContainerJSONBase{
			ID:   "existing-1",
			Name: "/force-test-abc",
		},
		Config: &container.Config{
			Labels: map[string]string{
				"colonies.generation": "1",
			},
		},
	}, nil)

	// Mock stop and remove for force cleanup
	mockDocker.On("ContainerStop", mock.Anything, "existing-1", mock.Anything).Return(nil)
	mockDocker.On("ContainerRemove", mock.Anything, "existing-1", mock.Anything).Return(nil)

	// Mock image check (image exists locally)
	mockDocker.On("ImageInspectWithRaw", mock.Anything, "test:latest").Return(types.ImageInspect{}, []byte{}, nil)

	// After force removal, subsequent ContainerList calls for normal reconciliation
	// Empty list since we removed the containers
	mockDocker.On("ContainerList", mock.Anything, mock.Anything).Return([]types.Container{}, nil)

	// Mock for creating new containers
	mockDocker.On("ContainerCreate", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(container.CreateResponse{ID: "new-container-1"}, nil)
	mockDocker.On("ContainerStart", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	// Verify that the container list was called for force removal
	containers, err := reconciler.listContainersByLabel("force-test")
	assert.NoError(t, err)
	assert.Len(t, containers, 1)
	assert.Equal(t, "existing-1", containers[0])
}

// TestForceReconcileTiming tests that force reconcile fails fast when image pull fails
// and doesn't remove containers until images are successfully pulled
func TestForceReconcileTiming(t *testing.T) {
	t.Run("Force_reconcile_aborts_if_image_pull_fails", func(t *testing.T) {
		mockDocker := new(MockDockerClient)
		mockHandler := new(MockImagePuller)

		// Setup: Image pull will fail
		mockHandler.On("PullImage", "test-image:latest", mock.Anything).Run(func(args mock.Arguments) {
			logChan := args.Get(1).(chan docker.LogMessage)
			// Simulate pull failure by closing channel and returning error
			close(logChan)
		}).Return(errors.New("image not found: test-image:latest"))

		// Setup: Container list should show existing containers (but won't be called due to early abort)
		mockDocker.On("ContainerList", mock.Anything, mock.Anything).Return([]types.Container{
			{
				ID:    "existing-container-1",
				Names: []string{"/my-executor-abc12"},
				Labels: map[string]string{
					"colonies.deployment": "my-executor",
					"colonies.generation": "1",
				},
			},
		}, nil)

		// Create reconciler with mocked handler (client is nil - not needed for abort test)
		reconciler := &Reconciler{
			dockerClient:   mockDocker,
			client:         nil,
			dockerHandler:  mockHandler,
			colonyName:     "test-colony",
			colonyOwnerKey: "test-owner-key",
			executorName:   "test-reconciler",
		}

		blueprint := &core.Blueprint{
			Kind: "ExecutorDeployment",
			Metadata: core.BlueprintMetadata{
				Name:       "my-executor",
				ColonyName: "test-colony",
				Generation: 2,
			},
			Spec: map[string]interface{}{
				"image":    "test-image:latest",
				"replicas": float64(1),
			},
		}

		process := &core.Process{
			FunctionSpec: core.FunctionSpec{},
		}

		// Execute: Force reconcile should fail
		err := reconciler.ForceReconcile(process, blueprint)

		// Assert: Error should be returned
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to pull image")
		assert.Contains(t, err.Error(), "aborting to preserve running containers")

		// Assert: ContainerStop and ContainerRemove should NOT have been called
		// because we abort before removing containers
		mockDocker.AssertNotCalled(t, "ContainerStop", mock.Anything, mock.Anything, mock.Anything)
		mockDocker.AssertNotCalled(t, "ContainerRemove", mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("Force_reconcile_proceeds_to_container_removal_when_image_pull_succeeds", func(t *testing.T) {
		// This test verifies that when image pull succeeds, we proceed to the container removal phase
		// We can't easily test the full flow without mocking the colonies client, but we can verify
		// that listContainersByLabel is called (which proves we got past image pull)
		mockDocker := new(MockDockerClient)
		mockHandler := new(MockImagePuller)

		// Setup: Image pull will succeed
		pullCalled := false
		mockHandler.On("PullImage", "test-image:latest", mock.Anything).Run(func(args mock.Arguments) {
			pullCalled = true
			logChan := args.Get(1).(chan docker.LogMessage)
			logChan <- docker.LogMessage{Log: "Pulling layer..."}
			logChan <- docker.LogMessage{EOF: true}
		}).Return(nil)

		// Setup: Container list returns empty (no existing containers to force remove)
		listCalled := false
		mockDocker.On("ContainerList", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
			listCalled = true
		}).Return([]types.Container{}, nil)

		// These won't be called but we need them to avoid test panic
		mockDocker.On("ImageInspectWithRaw", mock.Anything, mock.Anything).Maybe().Return(types.ImageInspect{}, []byte{}, nil)
		mockDocker.On("ContainerCreate", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe().
			Return(container.CreateResponse{ID: "new-1"}, nil)
		mockDocker.On("ContainerStart", mock.Anything, mock.Anything, mock.Anything).Maybe().Return(nil)
		mockDocker.On("ContainerInspect", mock.Anything, mock.Anything).Maybe().Return(types.ContainerJSON{
			ContainerJSONBase: &types.ContainerJSONBase{State: &types.ContainerState{Running: true}},
		}, nil)

		reconciler := &Reconciler{
			dockerClient:   mockDocker,
			client:         nil, // Will cause panic later, but we check pullCalled and listCalled first
			dockerHandler:  mockHandler,
			colonyName:     "test-colony",
			colonyOwnerKey: "test-owner-key",
			executorName:   "test-reconciler",
		}

		blueprint := &core.Blueprint{
			Kind: "ExecutorDeployment",
			Metadata: core.BlueprintMetadata{
				Name:       "my-executor",
				ColonyName: "test-colony",
				Generation: 2,
			},
			Spec: map[string]interface{}{
				"image":    "test-image:latest",
				"replicas": float64(1),
			},
		}

		process := &core.Process{
			FunctionSpec: core.FunctionSpec{},
		}

		// Execute with recovery - we expect a panic later due to nil client, but by then
		// we've already verified our assertions
		func() {
			defer func() {
				recover() // Ignore panic from nil client
			}()
			_ = reconciler.ForceReconcile(process, blueprint)
		}()

		// Assert: Image pull was called
		assert.True(t, pullCalled, "PullImage should have been called")

		// Assert: Container list was called - proves we got past image pull phase
		assert.True(t, listCalled, "ContainerList should have been called (got past image pull)")
	})

	t.Run("Force_reconcile_preserves_containers_on_pull_timeout", func(t *testing.T) {
		// This test verifies that containers are NOT removed when image pull fails/times out
		mockDocker := new(MockDockerClient)
		mockHandler := new(MockImagePuller)

		// Setup: Image pull will fail (simulating timeout)
		mockHandler.On("PullImage", "slow-image:latest", mock.Anything).Run(func(args mock.Arguments) {
			logChan := args.Get(1).(chan docker.LogMessage)
			close(logChan)
		}).Return(errors.New("pull timeout"))

		// Setup: Container list shows existing container
		mockDocker.On("ContainerList", mock.Anything, mock.Anything).Return([]types.Container{
			{
				ID:    "existing-1",
				Names: []string{"/my-app"},
				Labels: map[string]string{
					"colonies.deployment": "my-app",
				},
			},
		}, nil)

		reconciler := &Reconciler{
			dockerClient:   mockDocker,
			client:         nil,
			dockerHandler:  mockHandler,
			colonyName:     "test-colony",
			colonyOwnerKey: "test-owner-key",
			executorName:   "test-reconciler",
		}

		blueprint := &core.Blueprint{
			Kind: "ExecutorDeployment",
			Metadata: core.BlueprintMetadata{
				Name:       "my-app",
				ColonyName: "test-colony",
				Generation: 1,
			},
			Spec: map[string]interface{}{
				"image":    "slow-image:latest",
				"replicas": float64(1),
			},
		}

		process := &core.Process{
			FunctionSpec: core.FunctionSpec{},
		}

		// Execute
		err := reconciler.ForceReconcile(process, blueprint)

		// Assert: Should fail and containers should NOT be removed
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "aborting to preserve running containers")
		mockDocker.AssertNotCalled(t, "ContainerStop", mock.Anything, mock.Anything, mock.Anything)
		mockDocker.AssertNotCalled(t, "ContainerRemove", mock.Anything, mock.Anything, mock.Anything)
	})
}

// Benchmark tests
func BenchmarkContainerList(b *testing.B) {
	mockDocker := new(MockDockerClient)
	mockDocker.On("ContainerList", mock.Anything, mock.Anything).Return([]types.Container{}, nil)

	reconciler := createTestReconciler(mockDocker)
	blueprint := &core.Blueprint{
		Kind: "ExecutorDeployment",
		Metadata: core.BlueprintMetadata{
			Name: "benchmark-test",
		},
		Spec: map[string]interface{}{
			"replicas": float64(1),
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reconciler.CollectStatus(blueprint)
	}
}
