package reconciler

import (
	"context"
	"fmt"
	"time"

	"github.com/colonyos/colonies/pkg/core"
	log "github.com/sirupsen/logrus"
)

// CollectStatus gathers current status of instances for a blueprint
func (r *Reconciler) CollectStatus(blueprint *core.Blueprint) (map[string]interface{}, error) {
	// Get list of containers for this deployment
	containerIDs, err := r.listContainersByLabel(blueprint.Metadata.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	instances := make([]map[string]interface{}, 0)
	running := 0
	stopped := 0

	for _, containerID := range containerIDs {
		inspect, err := r.dockerClient.ContainerInspect(context.Background(), containerID)
		if err != nil {
			log.WithError(err).WithField("containerID", containerID).Warn("Failed to inspect container")
			continue
		}

		state := "stopped"
		if inspect.State.Running {
			state = "running"
			running++
		} else {
			stopped++
		}

		// Extract container name (remove leading "/")
		containerName := inspect.Name
		if len(containerName) > 0 && containerName[0] == '/' {
			containerName = containerName[1:]
		}

		instances = append(instances, map[string]interface{}{
			"id":        truncateID(inspect.ID, 12), // Short ID
			"name":      containerName,
			"type":      "container",
			"state":     state,
			"created":   inspect.Created,
			"image":     inspect.Config.Image,
			"lastCheck": time.Now().Format(time.RFC3339),
		})
	}

	return map[string]interface{}{
		"instances":        instances,
		"runningInstances": running,
		"stoppedInstances": stopped,
		"totalInstances":   len(instances),
		"lastUpdated":      time.Now().Format(time.RFC3339),
	}, nil
}

// GetDeploymentStatus returns the current status of containers for a deployment
func (r *Reconciler) GetDeploymentStatus(deploymentName string) (map[string]interface{}, error) {
	containers, err := r.listContainersByLabel(deploymentName)
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	status := map[string]interface{}{
		"deployment":      deploymentName,
		"currentReplicas": len(containers),
		"containerIDs":    containers,
	}

	return status, nil
}
