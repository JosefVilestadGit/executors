package executor

import (
	"fmt"

	"github.com/colonyos/colonies/pkg/core"
	log "github.com/sirupsen/logrus"
)

// checkReconciliationNeeded determines if a blueprint requires reconciliation
func (e *Executor) checkReconciliationNeeded(blueprint *core.Blueprint) (bool, string) {
	// Get current state from reconciler
	status, err := e.reconciler.CollectStatus(blueprint)
	if err != nil {
		log.WithFields(log.Fields{
			"Error":         err,
			"BlueprintName": blueprint.Metadata.Name,
		}).Warn("Failed to collect status")
		return false, ""
	}

	runningInstances, ok := status["runningInstances"].(int)
	if !ok {
		return false, ""
	}

	desiredReplicas := e.getDesiredReplicas(blueprint)
	if desiredReplicas < 0 {
		return false, ""
	}

	// Check replica count mismatch
	if runningInstances != desiredReplicas {
		return true, fmt.Sprintf("replica mismatch (running: %d, desired: %d)", runningInstances, desiredReplicas)
	}

	// Check for old generation containers
	hasOldGeneration, err := e.reconciler.HasOldGenerationContainers(blueprint)
	if err != nil {
		log.WithFields(log.Fields{
			"Error":         err,
			"BlueprintName": blueprint.Metadata.Name,
		}).Warn("Failed to check generation labels")
		return false, ""
	}

	if hasOldGeneration {
		return true, "containers with old generation labels detected"
	}

	return false, ""
}

// getDesiredReplicas extracts the desired replica count from a blueprint
func (e *Executor) getDesiredReplicas(blueprint *core.Blueprint) int {
	replicas, ok := blueprint.Spec["replicas"]
	if !ok {
		return -1
	}

	switch v := replicas.(type) {
	case int:
		return v
	case float64:
		return int(v)
	default:
		return -1
	}
}
