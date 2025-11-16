package executor

import (
	"fmt"

	"github.com/colonyos/colonies/pkg/core"
	log "github.com/sirupsen/logrus"
)

// performStartupReconciliation checks all managed blueprints on startup and reconciles if needed
// This ensures that the desired state is achieved even after executor restarts
func (e *Executor) performStartupReconciliation() {
	log.Info("Performing startup state reconciliation...")

	blueprints, err := e.fetchManagedBlueprints()
	if err != nil {
		log.WithFields(log.Fields{"Error": err}).Warn("Failed to fetch blueprints on startup")
		return
	}

	if len(blueprints) == 0 {
		log.Info("No blueprints found on startup")
		return
	}

	log.WithFields(log.Fields{"Count": len(blueprints)}).Info("Checking blueprints on startup")

	for _, blueprint := range blueprints {
		e.reconcileBlueprintIfNeeded(blueprint)
	}

	log.Info("Startup state reconciliation completed")
}

// fetchManagedBlueprints retrieves all ExecutorDeployment blueprints managed by docker-reconciler
func (e *Executor) fetchManagedBlueprints() ([]*core.Blueprint, error) {
	blueprints, err := e.client.GetBlueprints(e.colonyName, "ExecutorDeployment", e.colonyPrvKey)
	if err != nil {
		return nil, err
	}

	// Filter blueprints managed by docker-reconciler
	var managedBlueprints []*core.Blueprint
	for _, blueprint := range blueprints {
		if e.isManagedByReconciler(blueprint) {
			managedBlueprints = append(managedBlueprints, blueprint)
		}
	}

	return managedBlueprints, nil
}

// isManagedByReconciler checks if a blueprint is managed by this docker-reconciler
func (e *Executor) isManagedByReconciler(blueprint *core.Blueprint) bool {
	if blueprint.Spec == nil {
		return false
	}

	executorType, ok := blueprint.Spec["executorType"].(string)
	return ok && executorType == "docker-reconciler"
}

// reconcileBlueprintIfNeeded checks if a blueprint needs reconciliation and triggers it if necessary
func (e *Executor) reconcileBlueprintIfNeeded(blueprint *core.Blueprint) {
	needsReconciliation, reason := e.checkReconciliationNeeded(blueprint)

	if !needsReconciliation {
		desiredReplicas := e.getDesiredReplicas(blueprint)
		log.WithFields(log.Fields{
			"BlueprintName": blueprint.Metadata.Name,
			"Replicas":      desiredReplicas,
		}).Info("Blueprint already at desired state")
		return
	}

	log.WithFields(log.Fields{
		"BlueprintName": blueprint.Metadata.Name,
		"Reason":        reason,
		"Generation":    blueprint.Metadata.Generation,
	}).Info("Startup reconciliation needed")

	e.triggerReconciliation(blueprint)
}

// checkReconciliationNeeded determines if a blueprint requires reconciliation
func (e *Executor) checkReconciliationNeeded(blueprint *core.Blueprint) (bool, string) {
	// Get current state from reconciler
	status, err := e.reconciler.CollectStatus(blueprint)
	if err != nil {
		log.WithFields(log.Fields{
			"Error":         err,
			"BlueprintName": blueprint.Metadata.Name,
		}).Warn("Failed to collect status on startup")
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
		}).Warn("Failed to check generation labels on startup")
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

// triggerReconciliation creates and executes a reconciliation process for a blueprint
func (e *Executor) triggerReconciliation(blueprint *core.Blueprint) {
	// Create a reconciliation request
	reconciliation := &core.Reconciliation{
		Action: "update",
		New:    blueprint,
		Old:    blueprint,
	}

	// Create a synthetic process for reconciliation
	process := &core.Process{
		ID: "startup-reconcile-" + blueprint.Metadata.Name,
		FunctionSpec: core.FunctionSpec{
			Reconciliation: reconciliation,
		},
	}

	// Execute reconciliation
	if err := e.reconciler.Reconcile(process, blueprint); err != nil {
		log.WithFields(log.Fields{
			"Error":         err,
			"BlueprintName": blueprint.Metadata.Name,
		}).Error("Startup reconciliation failed")
		return
	}

	log.WithFields(log.Fields{
		"BlueprintName": blueprint.Metadata.Name,
	}).Info("Startup reconciliation completed successfully")

	// Track this blueprint as managed
	e.trackBlueprint(blueprint)
}

// trackBlueprint adds a blueprint to the managed resources map
func (e *Executor) trackBlueprint(blueprint *core.Blueprint) {
	e.resourcesMutex.Lock()
	defer e.resourcesMutex.Unlock()
	e.managedResources[blueprint.ID] = blueprint
}
