package executor

import (
	"sync"

	"github.com/colonyos/colonies/pkg/core"
	log "github.com/sirupsen/logrus"
)

// handleReconcile processes a reconciliation request
// Supports two modes:
// 1. Single blueprint: when "blueprintName" kwarg is present
// 2. Consolidated: when "kind" kwarg is present - fetches all blueprints of that Kind and reconciles in parallel
func (e *Executor) handleReconcile(process *core.Process) {
	log.WithFields(log.Fields{"ProcessID": process.ID}).Info("Handling blueprint reconciliation")

	// Check if this is a consolidated reconciliation (by Kind)
	if kind, ok := process.FunctionSpec.KwArgs["kind"].(string); ok {
		e.handleConsolidatedReconcile(process, kind)
		return
	}

	// Single blueprint reconciliation
	blueprintName, ok := process.FunctionSpec.KwArgs["blueprintName"].(string)
	if !ok {
		e.failProcess(process, "Neither 'kind' nor 'blueprintName' found in process kwargs")
		return
	}

	e.reconcileSingleBlueprint(process, blueprintName, true)
}

// handleConsolidatedReconcile fetches all blueprints of a Kind and reconciles them in parallel
func (e *Executor) handleConsolidatedReconcile(process *core.Process, kind string) {
	log.WithFields(log.Fields{
		"ProcessID": process.ID,
		"Kind":      kind,
	}).Info("Consolidated reconciliation for Kind")

	// Fetch all blueprints of this Kind from server
	blueprints, err := e.client.GetBlueprints(e.colonyName, kind, e.colonyPrvKey)
	if err != nil {
		e.failProcess(process, "Failed to fetch blueprints for kind "+kind+": "+err.Error())
		return
	}

	if len(blueprints) == 0 {
		log.WithFields(log.Fields{"Kind": kind}).Info("No blueprints found for Kind")
		e.client.Close(process.ID, e.executorPrvKey)
		return
	}

	log.WithFields(log.Fields{
		"Kind":  kind,
		"Count": len(blueprints),
	}).Info("Found blueprints to reconcile")

	// Reconcile all blueprints in parallel
	var wg sync.WaitGroup
	results := make(chan map[string]interface{}, len(blueprints))

	for _, blueprint := range blueprints {
		wg.Add(1)
		go func(bp *core.Blueprint) {
			defer wg.Done()
			result := e.reconcileBlueprintParallel(bp)
			results <- result
		}(blueprint)
	}

	// Wait for all reconciliations to complete
	wg.Wait()
	close(results)

	// Collect results
	var allResults []interface{}
	for result := range results {
		allResults = append(allResults, result)
	}

	// Close with aggregated results
	output := []interface{}{
		map[string]interface{}{
			"kind":    kind,
			"count":   len(blueprints),
			"results": allResults,
		},
	}

	if err := e.client.CloseWithOutput(process.ID, output, e.executorPrvKey); err != nil {
		log.WithFields(log.Fields{"Error": err}).Error("Failed to close consolidated reconcile process")
	} else {
		log.WithFields(log.Fields{
			"Kind":  kind,
			"Count": len(blueprints),
		}).Info("Consolidated reconciliation completed")
	}
}

// reconcileBlueprintParallel reconciles a single blueprint and returns the result (for parallel execution)
func (e *Executor) reconcileBlueprintParallel(blueprint *core.Blueprint) map[string]interface{} {
	result := map[string]interface{}{
		"blueprintName": blueprint.Metadata.Name,
		"success":       false,
	}

	// Check if reconciliation needed
	needsReconciliation, reason := e.checkReconciliationNeeded(blueprint)
	if !needsReconciliation {
		// Collect status even if no reconciliation needed
		status, err := e.reconciler.CollectStatus(blueprint)
		if err != nil {
			result["error"] = "Failed to collect status: " + err.Error()
			return result
		}
		result["success"] = true
		result["status"] = status
		result["action"] = "status_updated"
		return result
	}

	log.WithFields(log.Fields{
		"BlueprintName": blueprint.Metadata.Name,
		"Reason":        reason,
	}).Info("Reconciliation needed")

	// Perform reconciliation (using nil process since we don't want per-blueprint process tracking)
	if err := e.reconciler.Reconcile(nil, blueprint); err != nil {
		result["error"] = "Reconciliation failed: " + err.Error()
		return result
	}

	// Collect status after reconciliation
	status, err := e.reconciler.CollectStatus(blueprint)
	if err != nil {
		result["error"] = "Failed to collect status after reconcile: " + err.Error()
		return result
	}

	result["success"] = true
	result["status"] = status
	result["action"] = "reconciled"
	return result
}

// reconcileSingleBlueprint reconciles a single blueprint by name
func (e *Executor) reconcileSingleBlueprint(process *core.Process, blueprintName string, closeProcess bool) {
	// Fetch current blueprint from server
	blueprint, err := e.client.GetBlueprint(e.colonyName, blueprintName, e.colonyPrvKey)
	if err != nil {
		e.failProcess(process, "Failed to fetch blueprint: "+err.Error())
		return
	}

	log.WithFields(log.Fields{
		"BlueprintName": blueprint.Metadata.Name,
		"Generation":    blueprint.Metadata.Generation,
		"Kind":          blueprint.Kind,
	}).Info("Fetched blueprint from server")

	// Check if reconciliation needed
	needsReconciliation, reason := e.checkReconciliationNeeded(blueprint)
	if !needsReconciliation {
		log.WithFields(log.Fields{
			"BlueprintName": blueprint.Metadata.Name,
		}).Info("Blueprint already at desired state, skipping reconciliation")

		// Collect and report current status
		status, err := e.reconciler.CollectStatus(blueprint)
		if err != nil {
			log.WithFields(log.Fields{"Error": err}).Warn("Failed to collect status")
			if closeProcess {
				e.client.Close(process.ID, e.executorPrvKey)
			}
			return
		}

		if closeProcess {
			output := []interface{}{
				map[string]interface{}{
					"status": status,
				},
			}
			e.client.CloseWithOutput(process.ID, output, e.executorPrvKey)
		}
		return
	}

	log.WithFields(log.Fields{
		"BlueprintName": blueprint.Metadata.Name,
		"Reason":        reason,
		"Generation":    blueprint.Metadata.Generation,
	}).Info("Reconciliation needed")

	// Perform reconciliation
	if err := e.reconciler.Reconcile(process, blueprint); err != nil {
		e.failProcess(process, "Reconciliation failed: "+err.Error())
		return
	}

	// Collect status after successful reconciliation
	status, err := e.reconciler.CollectStatus(blueprint)
	if err != nil {
		log.WithFields(log.Fields{"Error": err}).Warn("Failed to collect status")
		if closeProcess {
			e.client.Close(process.ID, e.executorPrvKey)
		}
		return
	}

	if closeProcess {
		output := []interface{}{
			map[string]interface{}{
				"status": status,
			},
		}
		if err := e.client.CloseWithOutput(process.ID, output, e.executorPrvKey); err != nil {
			log.WithFields(log.Fields{"Error": err}).Error("Failed to close process with output")
		} else {
			log.WithFields(log.Fields{
				"BlueprintName":    blueprint.Metadata.Name,
				"Generation":       blueprint.Metadata.Generation,
				"RunningInstances": status["runningInstances"],
			}).Info("Reconciliation completed successfully")
		}
	}
}

// handleCleanup processes a cleanup request to remove containers for a deleted blueprint
func (e *Executor) handleCleanup(process *core.Process) {
	log.WithFields(log.Fields{"ProcessID": process.ID}).Info("Handling blueprint cleanup")

	// Extract blueprint name from process kwargs
	blueprintName, ok := process.FunctionSpec.KwArgs["blueprintName"].(string)
	if !ok {
		e.failProcess(process, "Blueprint name not found in process kwargs")
		return
	}

	log.WithFields(log.Fields{
		"BlueprintName": blueprintName,
	}).Info("Cleaning up containers for deleted blueprint")

	// Cleanup containers using the reconciler's cleanup method
	if err := e.reconciler.CleanupDeletedBlueprint(blueprintName); err != nil {
		e.failProcess(process, "Cleanup failed: "+err.Error())
		return
	}

	// Close process successfully
	output := []interface{}{
		map[string]interface{}{
			"status": "cleanup completed",
		},
	}

	if err := e.client.CloseWithOutput(process.ID, output, e.executorPrvKey); err != nil {
		log.WithFields(log.Fields{"Error": err}).Error("Failed to close cleanup process")
	} else {
		log.WithFields(log.Fields{
			"BlueprintName": blueprintName,
		}).Info("Cleanup completed successfully")
	}
}

// failProcess marks a process as failed with the given reason
func (e *Executor) failProcess(process *core.Process, reason string) {
	log.WithFields(log.Fields{"ProcessID": process.ID, "Reason": reason}).Error("Process failed")
	err := e.client.Fail(process.ID, []string{reason}, e.executorPrvKey)
	if err != nil {
		log.WithFields(log.Fields{"ProcessID": process.ID, "Error": err}).Error("Failed to mark process as failed")
	}
}
