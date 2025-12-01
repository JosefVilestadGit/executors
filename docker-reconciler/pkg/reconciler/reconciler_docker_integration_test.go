// +build integration

package reconciler

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/colonyos/colonies/pkg/client"
	"github.com/colonyos/colonies/pkg/core"
	"github.com/colonyos/executors/common/pkg/docker"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	dockerclient "github.com/docker/docker/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain sets up and tears down for integration tests
func TestMain(m *testing.M) {
	// Check if Docker is available
	cli, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv, dockerclient.WithAPIVersionNegotiation())
	if err != nil {
		fmt.Println("Skipping integration tests: Docker not available")
		os.Exit(0)
	}
	defer cli.Close()

	// Ping Docker to ensure it's running
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = cli.Ping(ctx)
	if err != nil {
		fmt.Println("Skipping integration tests: Docker daemon not responding")
		os.Exit(0)
	}

	// Run tests
	code := m.Run()

	// Cleanup any test containers
	cleanupTestContainers(cli)

	os.Exit(code)
}

func cleanupTestContainers(cli *dockerclient.Client) {
	ctx := context.Background()
	containers, err := cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return
	}

	for _, c := range containers {
		if deployment, ok := c.Labels["colonies.deployment"]; ok {
			if deployment == "integration-test" || deployment == "test-docker-integration" {
				cli.ContainerStop(ctx, c.ID, container.StopOptions{})
				cli.ContainerRemove(ctx, c.ID, container.RemoveOptions{Force: true})
			}
		}
	}
}

// createRealReconciler creates a reconciler with real Docker client (no Colonies client)
func createRealReconciler(t *testing.T) *Reconciler {
	dockerHandler, err := docker.CreateDockerHandler()
	require.NoError(t, err)

	dockerCli, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv, dockerclient.WithAPIVersionNegotiation())
	require.NoError(t, err)

	return &Reconciler{
		dockerHandler:  dockerHandler,
		dockerClient:   dockerCli,
		client:         nil, // No Colonies client for Docker-only tests
		executorPrvKey: "test-key",
		colonyOwnerKey: "test-owner-key",
		colonyName:     "test-colony",
		location:       "test-location",
	}
}

// createRealReconcilerWithColonies creates a reconciler with both Docker and Colonies clients
func createRealReconcilerWithColonies(t *testing.T) *Reconciler {
	dockerHandler, err := docker.CreateDockerHandler()
	require.NoError(t, err)

	dockerCli, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv, dockerclient.WithAPIVersionNegotiation())
	require.NoError(t, err)

	// Create Colonies client
	coloniesClient := client.CreateColoniesClient("localhost", 50080, true, false)

	return &Reconciler{
		dockerHandler:  dockerHandler,
		dockerClient:   dockerCli,
		client:         coloniesClient,
		executorPrvKey: "ddf7f7791208083b6a9ed975a72684f6406a269cfa36f1b1c32045c0a71fff05",
		colonyOwnerKey: "ba949fa134981372d6da62b6a56f336ab4d843b22c02a4257dcf7d0d73097514",
		colonyName:     "dev",
		location:       "test-location",
	}
}

// TestDockerIntegration_PullImage tests real image pulling
func TestDockerIntegration_PullImage(t *testing.T) {
	reconciler := createRealReconciler(t)
	process := &core.Process{ID: "test-pull-process"}

	// Use a small, commonly available image
	err := reconciler.pullImage(process, "alpine:latest")
	assert.NoError(t, err, "Should successfully pull alpine image")

	// Verify image exists
	ctx := context.Background()
	_, _, err = reconciler.dockerClient.ImageInspectWithRaw(ctx, "alpine:latest")
	assert.NoError(t, err, "Image should exist after pull")
}

// TestDockerIntegration_ContainerLifecycle tests full container lifecycle
func TestDockerIntegration_ContainerLifecycle(t *testing.T) {
	reconciler := createRealReconciler(t)

	// Ensure alpine image is available
	process := &core.Process{ID: "test-lifecycle-process"}
	err := reconciler.pullImage(process, "alpine:latest")
	require.NoError(t, err)

	ctx := context.Background()

	// Create container
	containerConfig := &container.Config{
		Image: "alpine:latest",
		Cmd:   []string{"sleep", "30"},
		Labels: map[string]string{
			"colonies.managed":    "true",
			"colonies.deployment": "integration-test",
			"colonies.generation": "1",
		},
	}

	hostConfig := &container.HostConfig{}

	resp, err := reconciler.dockerClient.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, "test-lifecycle-container")
	require.NoError(t, err, "Should create container")
	containerID := resp.ID

	// Cleanup at end
	defer func() {
		reconciler.dockerClient.ContainerStop(ctx, containerID, container.StopOptions{})
		reconciler.dockerClient.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true})
	}()

	// Start container
	err = reconciler.dockerClient.ContainerStart(ctx, containerID, container.StartOptions{})
	assert.NoError(t, err, "Should start container")

	// Wait for container to be running
	err = reconciler.waitForContainerRunning(containerID, 5*time.Second)
	assert.NoError(t, err, "Container should be running")

	// Verify container is running
	inspect, err := reconciler.dockerClient.ContainerInspect(ctx, containerID)
	require.NoError(t, err)
	assert.True(t, inspect.State.Running, "Container should be in running state")

	// Stop container
	err = reconciler.stopContainer(containerID)
	assert.NoError(t, err, "Should stop container")

	// Verify container is stopped
	inspect, err = reconciler.dockerClient.ContainerInspect(ctx, containerID)
	require.NoError(t, err)
	assert.False(t, inspect.State.Running, "Container should be stopped")

	// Remove container
	err = reconciler.stopAndRemoveContainer(containerID)
	assert.NoError(t, err, "Should remove container")

	// Verify container is gone
	_, err = reconciler.dockerClient.ContainerInspect(ctx, containerID)
	assert.Error(t, err, "Container should no longer exist")
}

// TestDockerIntegration_ListContainersByLabel tests container listing
func TestDockerIntegration_ListContainersByLabel(t *testing.T) {
	reconciler := createRealReconciler(t)
	process := &core.Process{ID: "test-list-process"}

	// Ensure alpine image
	err := reconciler.pullImage(process, "alpine:latest")
	require.NoError(t, err)

	ctx := context.Background()

	// Create 3 test containers
	containerIDs := []string{}
	for i := 0; i < 3; i++ {
		containerConfig := &container.Config{
			Image: "alpine:latest",
			Cmd:   []string{"sleep", "30"},
			Labels: map[string]string{
				"colonies.managed":    "true",
				"colonies.deployment": "test-docker-integration",
				"colonies.generation": "1",
			},
		}

		resp, err := reconciler.dockerClient.ContainerCreate(ctx, containerConfig, &container.HostConfig{}, nil, nil,
			fmt.Sprintf("test-list-%d", i))
		require.NoError(t, err)
		containerIDs = append(containerIDs, resp.ID)

		err = reconciler.dockerClient.ContainerStart(ctx, resp.ID, container.StartOptions{})
		require.NoError(t, err)
	}

	// Cleanup
	defer func() {
		for _, id := range containerIDs {
			reconciler.dockerClient.ContainerStop(ctx, id, container.StopOptions{})
			reconciler.dockerClient.ContainerRemove(ctx, id, container.RemoveOptions{Force: true})
		}
	}()

	// List containers by label
	foundIDs, err := reconciler.listContainersByLabel("test-docker-integration")
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, len(foundIDs), 3, "Should find at least 3 containers")

	// Verify all have correct label by inspecting each container
	for _, containerID := range foundIDs {
		inspect, err := reconciler.dockerClient.ContainerInspect(ctx, containerID)
		require.NoError(t, err)
		assert.Equal(t, "test-docker-integration", inspect.Config.Labels["colonies.deployment"])
	}
}

// TestDockerIntegration_HasOldGenerationContainers tests generation detection
func TestDockerIntegration_HasOldGenerationContainers(t *testing.T) {
	reconciler := createRealReconciler(t)
	process := &core.Process{ID: "test-generation-process"}

	err := reconciler.pullImage(process, "alpine:latest")
	require.NoError(t, err)

	ctx := context.Background()

	// Create container with old generation
	oldGenConfig := &container.Config{
		Image: "alpine:latest",
		Cmd:   []string{"sleep", "30"},
		Labels: map[string]string{
			"colonies.managed":    "true",
			"colonies.deployment": "test-generation-check",
			"colonies.generation": "1", // Old generation
		},
	}

	resp, err := reconciler.dockerClient.ContainerCreate(ctx, oldGenConfig, &container.HostConfig{}, nil, nil, "test-old-gen")
	require.NoError(t, err)
	oldContainerID := resp.ID

	err = reconciler.dockerClient.ContainerStart(ctx, oldContainerID, container.StartOptions{})
	require.NoError(t, err)

	// Cleanup
	defer func() {
		reconciler.dockerClient.ContainerStop(ctx, oldContainerID, container.StopOptions{})
		reconciler.dockerClient.ContainerRemove(ctx, oldContainerID, container.RemoveOptions{Force: true})
	}()

	// Check for old generation (current is 2)
	blueprint := &core.Blueprint{
		Metadata: core.BlueprintMetadata{
			Name:       "test-generation-check",
			Generation: 2, // Current generation
		},
	}

	hasOld, err := reconciler.HasOldGenerationContainers(blueprint)
	assert.NoError(t, err)
	assert.True(t, hasOld, "Should detect old generation container")
}

// TestDockerIntegration_CleanupOldGenerationContainers tests cleanup
func TestDockerIntegration_CleanupOldGenerationContainers(t *testing.T) {
	reconciler := createRealReconciler(t)
	process := &core.Process{ID: "test-cleanup-process"}

	err := reconciler.pullImage(process, "alpine:latest")
	require.NoError(t, err)

	ctx := context.Background()

	// Create containers with different generations
	oldGenConfig := &container.Config{
		Image: "alpine:latest",
		Cmd:   []string{"sleep", "30"},
		Labels: map[string]string{
			"colonies.managed":    "true",
			"colonies.deployment": "test-cleanup-deployment",
			"colonies.generation": "1",
		},
	}

	newGenConfig := &container.Config{
		Image: "alpine:latest",
		Cmd:   []string{"sleep", "30"},
		Labels: map[string]string{
			"colonies.managed":    "true",
			"colonies.deployment": "test-cleanup-deployment",
			"colonies.generation": "2",
		},
	}

	// Create old generation container
	oldResp, err := reconciler.dockerClient.ContainerCreate(ctx, oldGenConfig, &container.HostConfig{}, nil, nil, "test-cleanup-old")
	require.NoError(t, err)
	err = reconciler.dockerClient.ContainerStart(ctx, oldResp.ID, container.StartOptions{})
	require.NoError(t, err)

	// Create new generation container
	newResp, err := reconciler.dockerClient.ContainerCreate(ctx, newGenConfig, &container.HostConfig{}, nil, nil, "test-cleanup-new")
	require.NoError(t, err)
	err = reconciler.dockerClient.ContainerStart(ctx, newResp.ID, container.StartOptions{})
	require.NoError(t, err)

	// Cleanup at end
	defer func() {
		reconciler.dockerClient.ContainerStop(ctx, oldResp.ID, container.StopOptions{})
		reconciler.dockerClient.ContainerRemove(ctx, oldResp.ID, container.RemoveOptions{Force: true})
		reconciler.dockerClient.ContainerStop(ctx, newResp.ID, container.StopOptions{})
		reconciler.dockerClient.ContainerRemove(ctx, newResp.ID, container.RemoveOptions{Force: true})
	}()

	blueprint := &core.Blueprint{
		Metadata: core.BlueprintMetadata{
			Name:       "test-cleanup-deployment",
			Generation: 2,
		},
	}

	// Cleanup old generation
	err = reconciler.CleanupOldGenerationContainers(nil, blueprint)
	assert.NoError(t, err)

	// Wait a bit for cleanup
	time.Sleep(1 * time.Second)

	// Verify old container is gone
	_, err = reconciler.dockerClient.ContainerInspect(ctx, oldResp.ID)
	assert.Error(t, err, "Old generation container should be removed")

	// Verify new container still exists
	inspect, err := reconciler.dockerClient.ContainerInspect(ctx, newResp.ID)
	assert.NoError(t, err, "New generation container should still exist")
	assert.True(t, inspect.State.Running, "New container should still be running")
}

// TestDockerIntegration_CleanupStoppedContainers tests stopped container cleanup
func TestDockerIntegration_CleanupStoppedContainers(t *testing.T) {
	reconciler := createRealReconciler(t)
	process := &core.Process{ID: "test-stopped-cleanup"}

	err := reconciler.pullImage(process, "alpine:latest")
	require.NoError(t, err)

	ctx := context.Background()

	// Create and immediately stop a container
	containerConfig := &container.Config{
		Image: "alpine:latest",
		Cmd:   []string{"echo", "test"},
		Labels: map[string]string{
			"colonies.managed":    "true",
			"colonies.deployment": "test-stopped-cleanup",
		},
	}

	resp, err := reconciler.dockerClient.ContainerCreate(ctx, containerConfig, &container.HostConfig{}, nil, nil, "test-stopped")
	require.NoError(t, err)

	// Start and let it exit
	err = reconciler.dockerClient.ContainerStart(ctx, resp.ID, container.StartOptions{})
	require.NoError(t, err)

	// Wait for container to exit
	time.Sleep(2 * time.Second)

	// Cleanup stopped containers
	err = reconciler.CleanupStoppedContainers(nil)
	assert.NoError(t, err)

	// Wait for cleanup
	time.Sleep(1 * time.Second)

	// Verify container is removed
	_, err = reconciler.dockerClient.ContainerInspect(ctx, resp.ID)
	assert.Error(t, err, "Stopped container should be removed")
}

// TestDockerIntegration_CollectStatus tests real status collection
func TestDockerIntegration_CollectStatus(t *testing.T) {
	reconciler := createRealReconciler(t)
	process := &core.Process{ID: "test-status-process"}

	err := reconciler.pullImage(process, "alpine:latest")
	require.NoError(t, err)

	ctx := context.Background()

	// Create 2 running and 1 stopped container
	containerIDs := []string{}

	for i := 0; i < 2; i++ {
		containerConfig := &container.Config{
			Image: "alpine:latest",
			Cmd:   []string{"sleep", "60"},
			Labels: map[string]string{
				"colonies.managed":    "true",
				"colonies.deployment": "test-status-collection",
				"colonies.generation": "1",
			},
		}

		resp, err := reconciler.dockerClient.ContainerCreate(ctx, containerConfig, &container.HostConfig{}, nil, nil,
			fmt.Sprintf("test-status-running-%d", i))
		require.NoError(t, err)
		containerIDs = append(containerIDs, resp.ID)

		err = reconciler.dockerClient.ContainerStart(ctx, resp.ID, container.StartOptions{})
		require.NoError(t, err)
	}

	// Create stopped container
	stoppedConfig := &container.Config{
		Image: "alpine:latest",
		Cmd:   []string{"echo", "done"},
		Labels: map[string]string{
			"colonies.managed":    "true",
			"colonies.deployment": "test-status-collection",
			"colonies.generation": "1",
		},
	}

	stoppedResp, err := reconciler.dockerClient.ContainerCreate(ctx, stoppedConfig, &container.HostConfig{}, nil, nil, "test-status-stopped")
	require.NoError(t, err)
	containerIDs = append(containerIDs, stoppedResp.ID)

	// Start and let it exit
	err = reconciler.dockerClient.ContainerStart(ctx, stoppedResp.ID, container.StartOptions{})
	require.NoError(t, err)
	time.Sleep(2 * time.Second)

	// Cleanup
	defer func() {
		for _, id := range containerIDs {
			reconciler.dockerClient.ContainerStop(ctx, id, container.StopOptions{})
			reconciler.dockerClient.ContainerRemove(ctx, id, container.RemoveOptions{Force: true})
		}
	}()

	// Collect status
	blueprint := &core.Blueprint{
		Kind: "ExecutorDeployment",
		Metadata: core.BlueprintMetadata{
			Name:       "test-status-collection",
			Generation: 1,
		},
		Spec: map[string]interface{}{
			"replicas": float64(3),
		},
	}

	status, err := reconciler.CollectStatus(blueprint)
	assert.NoError(t, err)
	assert.NotNil(t, status)

	// Should have 2 running, 1 stopped
	assert.Equal(t, 2, status["runningInstances"], "Should have 2 running containers")
	assert.Equal(t, 1, status["stoppedInstances"], "Should have 1 stopped container")
	assert.Equal(t, 3, status["totalInstances"], "Should have 3 total containers")
}

// TestDockerIntegration_ExecutorDeployment tests executor deployment with real Colonies server
func TestDockerIntegration_ExecutorDeployment(t *testing.T) {
	reconciler := createRealReconcilerWithColonies(t)
	ctx := context.Background()

	// Create blueprint for executor deployment
	blueprint := &core.Blueprint{
		Kind: "ExecutorDeployment",
		Metadata: core.BlueprintMetadata{
			Name:       "test-executor-integration",
			Generation: 1,
		},
		Spec: map[string]interface{}{
			"replicas":     float64(2),
			"image":        "alpine:latest",
			"cmd":          []interface{}{"sleep", "60"},
			"executorType": "test-executor",
		},
	}

	// Create a process for the reconciliation
	process := &core.Process{ID: "test-executor-deployment-process"}

	// Cleanup any existing containers
	defer func() {
		filterArgs := filters.NewArgs()
		filterArgs.Add("label", "colonies.deployment=test-executor-integration")
		containers, _ := reconciler.dockerClient.ContainerList(ctx, container.ListOptions{
			All:     true,
			Filters: filterArgs,
		})
		for _, c := range containers {
			reconciler.dockerClient.ContainerStop(ctx, c.ID, container.StopOptions{})
			reconciler.dockerClient.ContainerRemove(ctx, c.ID, container.RemoveOptions{Force: true})
		}
	}()

	// Reconcile - should create 2 executor containers
	err := reconciler.reconcileExecutorDeployment(process, blueprint)
	assert.NoError(t, err, "Should reconcile executor deployment")

	// Wait for containers to start
	time.Sleep(3 * time.Second)

	// Verify containers were created
	status, err := reconciler.CollectStatus(blueprint)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, status["runningInstances"], 1, "Should have at least 1 running container")
}

// TestDockerIntegration_StoppedContainersNotCountedAsReplicas tests the bug fix where
// stopped containers were incorrectly counted as current replicas, causing wrong scaling decisions
func TestDockerIntegration_StoppedContainersNotCountedAsReplicas(t *testing.T) {
	reconciler := createRealReconciler(t)
	ctx := context.Background()

	// Create blueprint with replicas: 1
	// Use DockerDeployment to avoid executor registration (which needs Colonies client)
	blueprint := &core.Blueprint{
		Metadata: core.BlueprintMetadata{
			Name:       "stopped-test-deployment",
			Namespace:  "test",
			Generation: 1,
		},
		Kind: "DockerDeployment",
		Spec: map[string]interface{}{
			"instances": []interface{}{
				map[string]interface{}{
					"name":    "test-instance",
					"image":   "alpine:latest",
					"command": []string{"sleep", "300"},
				},
			},
		},
	}

	// Create synthetic process for testing
	process := &core.Process{
		ID: "test-stopped-containers-process",
	}

	// Cleanup function
	defer func() {
		filterArgs := filters.NewArgs()
		filterArgs.Add("label", "colonies.deployment="+blueprint.Metadata.Name)
		containers, _ := reconciler.dockerClient.ContainerList(ctx, container.ListOptions{
			All:     true,
			Filters: filterArgs,
		})
		for _, c := range containers {
			reconciler.dockerClient.ContainerStop(ctx, c.ID, container.StopOptions{})
			reconciler.dockerClient.ContainerRemove(ctx, c.ID, container.RemoveOptions{Force: true})
		}
	}()

	// Manually create containers to simulate the bug scenario:
	// 1 running container + 2 stopped containers (like after a rebuild)

	// Create 2 stopped containers (old generation 0)
	for i := 0; i < 2; i++ {
		config := &container.Config{
			Image: "alpine:latest",
			Cmd:   []string{"echo", "stopped"},
			Labels: map[string]string{
				"colonies.deployment": blueprint.Metadata.Name,
				"colonies.generation": "0",
				"colonies.managed":    "true",
			},
		}
		hostConfig := &container.HostConfig{}
		resp, err := reconciler.dockerClient.ContainerCreate(ctx, config, hostConfig, nil, nil, fmt.Sprintf("stopped-container-%d", i))
		require.NoError(t, err)

		// Start and immediately stop to create a stopped container
		err = reconciler.dockerClient.ContainerStart(ctx, resp.ID, container.StartOptions{})
		require.NoError(t, err)

		time.Sleep(100 * time.Millisecond)
		reconciler.dockerClient.ContainerStop(ctx, resp.ID, container.StopOptions{})
	}

	// Create 1 running container (current generation 1) with the expected instance name
	runningConfig := &container.Config{
		Image: "alpine:latest",
		Cmd:   []string{"sleep", "300"},
		Labels: map[string]string{
			"colonies.deployment": blueprint.Metadata.Name,
			"colonies.generation": "1",
			"colonies.managed":    "true",
		},
	}
	runningHostConfig := &container.HostConfig{}
	runningResp, err := reconciler.dockerClient.ContainerCreate(ctx, runningConfig, runningHostConfig, nil, nil, "test-instance")
	require.NoError(t, err)
	err = reconciler.dockerClient.ContainerStart(ctx, runningResp.ID, container.StartOptions{})
	require.NoError(t, err)

	time.Sleep(500 * time.Millisecond)

	// Verify initial state:
	// - Total containers (running + stopped): 3
	// - Running containers: 1
	// - Stopped containers: 2
	allContainers, err := reconciler.listContainersByLabel(blueprint.Metadata.Name)
	require.NoError(t, err)
	assert.Equal(t, 3, len(allContainers), "Should have 3 total containers (1 running + 2 stopped)")

	runningOnly, err := reconciler.listRunningContainersByLabel(blueprint.Metadata.Name)
	require.NoError(t, err)
	assert.Equal(t, 1, len(runningOnly), "Should have exactly 1 running container")

	// Now reconcile - with the bug fix, it should:
	// 1. Count only running containers (1)
	// 2. Compare with desired instances (1)
	// 3. Not try to remove existing containers
	// 4. Keep the running container alive
	err = reconciler.Reconcile(process, blueprint)
	assert.NoError(t, err, "Reconciliation should succeed")

	// Verify post-reconciliation state:
	// The running container should still be running
	finalRunning, err := reconciler.listRunningContainersByLabel(blueprint.Metadata.Name)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(finalRunning), 1, "Should still have at least 1 running container after reconciliation")

	// Verify the running container is still the same one or a new one (not stopped)
	_, err = reconciler.dockerClient.ContainerInspect(ctx, runningResp.ID)
	if err == nil {
		// Original container might have been recreated if it was dirty, check if any container is running
		assert.GreaterOrEqual(t, len(finalRunning), 1, "Should have running container")
	}

	// Collect status and verify
	status, err := reconciler.CollectStatus(blueprint)
	require.NoError(t, err)
	assert.Equal(t, 1, status["runningInstances"], "Should have exactly 1 running instance")

	// Key assertion: The reconciler should NOT have treated stopped containers as replicas
	// If it did, it would have tried to scale down from 3 to 1, potentially removing the running container
	// With the fix, it correctly counts 1 running container and maintains 1 replica
	t.Logf("✅ Bug fix verified: Stopped containers not counted as replicas")
	t.Logf("   Total containers (all): %d", len(allContainers))
	t.Logf("   Running containers: %d", status["runningInstances"])
	t.Logf("   Stopped containers: %d", status["stoppedInstances"])
}

// cleanupDeploymentContainers removes all containers for a specific deployment
func cleanupDeploymentContainers(cli DockerClient, deploymentName string) {
	ctx := context.Background()
	containers, err := cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return
	}

	for _, c := range containers {
		if deployment, ok := c.Labels["colonies.deployment"]; ok {
			if deployment == deploymentName {
				cli.ContainerStop(ctx, c.ID, container.StopOptions{})
				cli.ContainerRemove(ctx, c.ID, container.RemoveOptions{Force: true})
			}
		}
	}
}

// TestDockerIntegration_DockerDeployment_SingleReplica tests single replica deployment
func TestDockerIntegration_DockerDeployment_SingleReplica(t *testing.T) {
	reconciler := createRealReconciler(t)
	ctx := context.Background()

	// Setup cleanup
	defer cleanupDeploymentContainers(reconciler.dockerClient, "test-single-replica")

	// Create a blueprint with single replica
	blueprint := &core.Blueprint{
		Metadata: core.BlueprintMetadata{
			Name:       "test-single-replica",
			Generation: 1,
		},
		Spec: map[string]interface{}{
			"replicas": 1,
			"instances": []interface{}{
				map[string]interface{}{
					"name":  "web-server",
					"type":  "container",
					"image": "alpine:latest",
					"command": []interface{}{
						"sleep", "30",
					},
					"environment": map[string]interface{}{
						"TEST_VAR": "test-value",
					},
				},
			},
		},
	}

	// Create a process for logging
	process := &core.Process{ID: "test-single-replica-process"}

	// Reconcile the deployment
	err := reconciler.reconcileDockerDeployment(process, blueprint)
	require.NoError(t, err, "Single replica deployment should succeed")

	// Verify exactly 1 container was created
	containers, err := reconciler.listContainersByLabel(blueprint.Metadata.Name)
	require.NoError(t, err)
	assert.Equal(t, 1, len(containers), "Should have exactly 1 container")

	// Verify container name is just the instance name (no suffix)
	inspect, err := reconciler.dockerClient.ContainerInspect(ctx, containers[0])
	require.NoError(t, err)
	containerName := inspect.Name
	if len(containerName) > 0 && containerName[0] == '/' {
		containerName = containerName[1:]
	}
	assert.Equal(t, "web-server", containerName, "Single replica should use instance name without suffix")

	// Verify container has correct generation label
	assert.Equal(t, "1", inspect.Config.Labels["colonies.generation"])
	assert.Equal(t, "test-single-replica", inspect.Config.Labels["colonies.deployment"])
}

// TestDockerIntegration_DockerDeployment_MultipleReplicas tests multiple replicas deployment
func TestDockerIntegration_DockerDeployment_MultipleReplicas(t *testing.T) {
	reconciler := createRealReconciler(t)
	ctx := context.Background()

	// Setup cleanup
	defer cleanupDeploymentContainers(reconciler.dockerClient, "test-multi-replica")

	// Create a blueprint with 3 replicas
	blueprint := &core.Blueprint{
		Metadata: core.BlueprintMetadata{
			Name:       "test-multi-replica",
			Generation: 1,
		},
		Spec: map[string]interface{}{
			"replicas": 3,
			"instances": []interface{}{
				map[string]interface{}{
					"name":  "app-server",
					"type":  "container",
					"image": "alpine:latest",
					"command": []interface{}{
						"sleep", "30",
					},
				},
			},
		},
	}

	process := &core.Process{ID: "test-multi-replica-process"}

	// Reconcile the deployment
	err := reconciler.reconcileDockerDeployment(process, blueprint)
	require.NoError(t, err, "Multiple replicas deployment should succeed")

	// Verify exactly 3 containers were created
	containers, err := reconciler.listContainersByLabel(blueprint.Metadata.Name)
	require.NoError(t, err)
	assert.Equal(t, 3, len(containers), "Should have exactly 3 containers")

	// Verify container names are numbered: app-server-0, app-server-1, app-server-2
	expectedNames := map[string]bool{
		"app-server-0": false,
		"app-server-1": false,
		"app-server-2": false,
	}

	for _, containerID := range containers {
		inspect, err := reconciler.dockerClient.ContainerInspect(ctx, containerID)
		require.NoError(t, err)

		containerName := inspect.Name
		if len(containerName) > 0 && containerName[0] == '/' {
			containerName = containerName[1:]
		}

		_, exists := expectedNames[containerName]
		assert.True(t, exists, "Container name %s should be one of the expected replica names", containerName)
		expectedNames[containerName] = true

		// Verify labels
		assert.Equal(t, "1", inspect.Config.Labels["colonies.generation"])
		assert.Equal(t, "test-multi-replica", inspect.Config.Labels["colonies.deployment"])
		assert.True(t, inspect.State.Running, "Container should be running")
	}

	// Verify all expected names were found
	for name, found := range expectedNames {
		assert.True(t, found, "Expected container %s should exist", name)
	}
}

// TestDockerIntegration_DockerDeployment_DefaultReplicas tests default replica count
func TestDockerIntegration_DockerDeployment_DefaultReplicas(t *testing.T) {
	reconciler := createRealReconciler(t)

	// Setup cleanup
	defer cleanupDeploymentContainers(reconciler.dockerClient, "test-default-replica")

	// Create a blueprint without specifying replicas (should default to instance count)
	blueprint := &core.Blueprint{
		Metadata: core.BlueprintMetadata{
			Name:       "test-default-replica",
			Generation: 1,
		},
		Spec: map[string]interface{}{
			// No replicas field specified
			"instances": []interface{}{
				map[string]interface{}{
					"name":  "instance1",
					"type":  "container",
					"image": "alpine:latest",
					"command": []interface{}{
						"sleep", "30",
					},
				},
				map[string]interface{}{
					"name":  "instance2",
					"type":  "container",
					"image": "alpine:latest",
					"command": []interface{}{
						"sleep", "30",
					},
				},
			},
		},
	}

	process := &core.Process{ID: "test-default-replica-process"}

	// Reconcile the deployment
	err := reconciler.reconcileDockerDeployment(process, blueprint)
	require.NoError(t, err, "Default replicas deployment should succeed")

	// Verify 4 containers were created (2 replicas per instance, since replicas defaults to instance count when not specified)
	containers, err := reconciler.listContainersByLabel(blueprint.Metadata.Name)
	require.NoError(t, err)
	assert.Equal(t, 4, len(containers), "Should have exactly 4 containers (2 replicas × 2 instances)")
}

// TestDockerIntegration_DockerDeployment_ReplicaScaling tests scaling replicas
func TestDockerIntegration_DockerDeployment_ReplicaScaling(t *testing.T) {
	reconciler := createRealReconciler(t)
	ctx := context.Background()

	// Setup cleanup
	defer cleanupDeploymentContainers(reconciler.dockerClient, "test-scaling")

	// Phase 1: Start with 2 replicas
	blueprint := &core.Blueprint{
		Metadata: core.BlueprintMetadata{
			Name:       "test-scaling",
			Generation: 1,
		},
		Spec: map[string]interface{}{
			"replicas": 2,
			"instances": []interface{}{
				map[string]interface{}{
					"name":  "scalable-app",
					"type":  "container",
					"image": "alpine:latest",
					"command": []interface{}{
						"sleep", "60",
					},
				},
			},
		},
	}

	process := &core.Process{ID: "test-scaling-process"}

	// Initial reconciliation with 2 replicas
	err := reconciler.reconcileDockerDeployment(process, blueprint)
	require.NoError(t, err, "Initial deployment with 2 replicas should succeed")

	// Verify 2 containers exist
	containers, err := reconciler.listContainersByLabel(blueprint.Metadata.Name)
	require.NoError(t, err)
	assert.Equal(t, 2, len(containers), "Should have exactly 2 containers initially")

	// Phase 2: Scale up to 4 replicas
	blueprint.Spec["replicas"] = 4
	blueprint.Metadata.Generation = 2

	err = reconciler.reconcileDockerDeployment(process, blueprint)
	require.NoError(t, err, "Scaling up to 4 replicas should succeed")

	// Verify 4 containers now exist
	containers, err = reconciler.listContainersByLabel(blueprint.Metadata.Name)
	require.NoError(t, err)
	assert.Equal(t, 4, len(containers), "Should have exactly 4 containers after scaling up")

	// Verify all container names
	expectedNames := map[string]bool{
		"scalable-app-0": false,
		"scalable-app-1": false,
		"scalable-app-2": false,
		"scalable-app-3": false,
	}

	for _, containerID := range containers {
		inspect, err := reconciler.dockerClient.ContainerInspect(ctx, containerID)
		require.NoError(t, err)

		containerName := inspect.Name
		if len(containerName) > 0 && containerName[0] == '/' {
			containerName = containerName[1:]
		}

		expectedNames[containerName] = true
		assert.True(t, inspect.State.Running, "Container %s should be running", containerName)
	}

	for name, found := range expectedNames {
		assert.True(t, found, "Expected container %s should exist", name)
	}

	// Phase 3: Scale down to 2 replicas
	blueprint.Spec["replicas"] = 2
	blueprint.Metadata.Generation = 3

	err = reconciler.reconcileDockerDeployment(process, blueprint)
	require.NoError(t, err, "Scaling down to 2 replicas should succeed")

	// Verify only 2 containers remain
	containers, err = reconciler.listContainersByLabel(blueprint.Metadata.Name)
	require.NoError(t, err)
	assert.Equal(t, 2, len(containers), "Should have exactly 2 containers after scaling down")
}

// TestDockerIntegration_DockerDeployment_GenerationUpdate tests generation updates with replicas
func TestDockerIntegration_DockerDeployment_GenerationUpdate(t *testing.T) {
	reconciler := createRealReconciler(t)
	ctx := context.Background()

	// Setup cleanup
	defer cleanupDeploymentContainers(reconciler.dockerClient, "test-generation-update")

	// Phase 1: Create initial deployment with 3 replicas at generation 1
	blueprint := &core.Blueprint{
		Metadata: core.BlueprintMetadata{
			Name:       "test-generation-update",
			Generation: 1,
		},
		Spec: map[string]interface{}{
			"replicas": 3,
			"instances": []interface{}{
				map[string]interface{}{
					"name":  "versioned-app",
					"type":  "container",
					"image": "alpine:latest",
					"command": []interface{}{
						"sleep", "60",
					},
					"environment": map[string]interface{}{
						"VERSION": "1.0",
					},
				},
			},
		},
	}

	process := &core.Process{ID: "test-generation-update-process"}

	// Initial reconciliation
	err := reconciler.reconcileDockerDeployment(process, blueprint)
	require.NoError(t, err, "Initial deployment should succeed")

	// Verify 3 containers with generation 1
	containers, err := reconciler.listContainersByLabel(blueprint.Metadata.Name)
	require.NoError(t, err)
	assert.Equal(t, 3, len(containers), "Should have 3 containers")

	oldContainerIDs := make(map[string]bool)
	for _, containerID := range containers {
		inspect, err := reconciler.dockerClient.ContainerInspect(ctx, containerID)
		require.NoError(t, err)
		assert.Equal(t, "1", inspect.Config.Labels["colonies.generation"], "Should have generation 1")
		oldContainerIDs[containerID] = true
	}

	// Phase 2: Update to generation 2 with different environment
	blueprint.Metadata.Generation = 2
	instances := blueprint.Spec["instances"].([]interface{})
	firstInstance := instances[0].(map[string]interface{})
	firstInstance["environment"] = map[string]interface{}{
		"VERSION": "2.0",
	}

	err = reconciler.reconcileDockerDeployment(process, blueprint)
	require.NoError(t, err, "Generation update should succeed")

	// Verify all 3 containers were recreated with generation 2
	containers, err = reconciler.listContainersByLabel(blueprint.Metadata.Name)
	require.NoError(t, err)
	assert.Equal(t, 3, len(containers), "Should still have 3 containers")

	newContainerCount := 0
	for _, containerID := range containers {
		inspect, err := reconciler.dockerClient.ContainerInspect(ctx, containerID)
		require.NoError(t, err)

		// Verify generation label is updated
		assert.Equal(t, "2", inspect.Config.Labels["colonies.generation"], "Should have generation 2")

		// Verify these are new containers (different IDs)
		if !oldContainerIDs[containerID] {
			newContainerCount++
		}

		// Verify environment variable was updated
		envFound := false
		for _, env := range inspect.Config.Env {
			if env == "VERSION=2.0" {
				envFound = true
				break
			}
		}
		assert.True(t, envFound, "Should have updated VERSION environment variable")
	}

	assert.Equal(t, 3, newContainerCount, "All 3 containers should be new (recreated)")
}
