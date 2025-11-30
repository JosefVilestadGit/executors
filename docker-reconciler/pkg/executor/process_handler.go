package executor

import (
	"fmt"
	"sync"

	"github.com/colonyos/colonies/pkg/core"
	log "github.com/sirupsen/logrus"
)

// handleReconcile processes a reconciliation request
// Only supports consolidated reconciliation by Kind (cron-based mode)
func (e *Executor) handleReconcile(process *core.Process) {
	log.WithFields(log.Fields{"ProcessID": process.ID}).Info("Handling blueprint reconciliation")

	// Consolidated reconciliation (by Kind)
	kind, ok := process.FunctionSpec.KwArgs["kind"].(string)
	if !ok {
		errMsg := "'kind' kwarg not found in process - only consolidated reconciliation is supported"
		e.addProcessLog(process, "Error: "+errMsg)
		e.failProcess(process, errMsg)
		return
	}

	e.handleConsolidatedReconcile(process, kind)
}

// handleConsolidatedReconcile fetches all blueprints of a Kind and reconciles them in parallel
// If blueprintName is provided, only that specific blueprint is reconciled
func (e *Executor) handleConsolidatedReconcile(process *core.Process, kind string) {
	// Check for specific blueprint name (single blueprint reconciliation)
	blueprintName := ""
	if name, ok := process.FunctionSpec.KwArgs["blueprintName"].(string); ok {
		blueprintName = name
	}

	log.WithFields(log.Fields{
		"ProcessID":     process.ID,
		"Kind":          kind,
		"BlueprintName": blueprintName,
	}).Info("Reconciliation request")

	// Check for force flag in kwargs
	force := false
	if forceVal, ok := process.FunctionSpec.KwArgs["force"].(bool); ok {
		force = forceVal
	}

	// Add log to process for visibility via `colonies log get`
	if blueprintName != "" {
		e.addProcessLog(process, "Starting reconciliation for blueprint: "+blueprintName)
	} else {
		e.addProcessLog(process, "Starting reconciliation for Kind: "+kind)
	}
	if force {
		e.addProcessLog(process, "Force flag enabled - will recreate all containers")
	}

	// Fetch all blueprints of this Kind from server
	allBlueprints, err := e.client.GetBlueprints(e.colonyName, kind, e.colonyPrvKey)
	if err != nil {
		errMsg := fmt.Sprintf("Failed to fetch blueprints for kind %s: %v", kind, err)
		e.addProcessLog(process, errMsg)
		e.failProcess(process, errMsg)
		return
	}

	// Filter blueprints that are assigned to this executor
	// If blueprintName is specified, also filter to only that blueprint
	var blueprints []*core.Blueprint
	for _, blueprint := range allBlueprints {
		// If a specific blueprint name is requested, skip others
		if blueprintName != "" && blueprint.Metadata.Name != blueprintName {
			continue
		}
		if e.shouldHandleBlueprint(blueprint) {
			blueprints = append(blueprints, blueprint)
		} else {
			log.WithFields(log.Fields{
				"BlueprintName":   blueprint.Metadata.Name,
				"HandlerExecutor": getHandlerExecutorName(blueprint),
				"MyExecutorName":  e.executorName,
			}).Debug("Skipping blueprint not assigned to this executor")
		}
	}

	if len(blueprints) == 0 {
		log.WithFields(log.Fields{"Kind": kind, "Total": len(allBlueprints)}).Info("No blueprints assigned to this executor for Kind")
		e.addProcessLog(process, fmt.Sprintf("No blueprints assigned to this executor for Kind: %s (total: %d)", kind, len(allBlueprints)))
		e.client.Close(process.ID, e.executorPrvKey)
		return
	}

	log.WithFields(log.Fields{
		"Kind":  kind,
		"Count": len(blueprints),
		"Total": len(allBlueprints),
	}).Info("Found blueprints to reconcile")
	e.addProcessLog(process, fmt.Sprintf("Found %d blueprint(s) to reconcile (of %d total)", len(blueprints), len(allBlueprints)))

	// Reconcile all blueprints in parallel
	var wg sync.WaitGroup
	results := make(chan map[string]interface{}, len(blueprints))

	for _, blueprint := range blueprints {
		wg.Add(1)
		go func(bp *core.Blueprint) {
			defer wg.Done()
			result := e.reconcileBlueprintParallel(process, bp, force)
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

	e.addProcessLog(process, fmt.Sprintf("Reconciliation completed for all %d blueprint(s)", len(blueprints)))

	if err := e.client.CloseWithOutput(process.ID, output, e.executorPrvKey); err != nil {
		log.WithFields(log.Fields{"Error": err}).Error("Failed to close consolidated reconcile process")
		e.addProcessLog(process, fmt.Sprintf("Error closing process: %v", err))
	} else {
		log.WithFields(log.Fields{
			"Kind":  kind,
			"Count": len(blueprints),
		}).Info("Consolidated reconciliation completed")
	}
}

// reconcileBlueprintParallel reconciles a single blueprint and returns the result (for parallel execution)
func (e *Executor) reconcileBlueprintParallel(process *core.Process, blueprint *core.Blueprint, force bool) map[string]interface{} {
	result := map[string]interface{}{
		"blueprintName": blueprint.Metadata.Name,
		"success":       false,
	}

	e.addProcessLog(process, fmt.Sprintf("Processing blueprint: %s", blueprint.Metadata.Name))

	// If force flag is set, perform force reconciliation (recreate containers)
	if force {
		e.addProcessLog(process, fmt.Sprintf("Force reconciling blueprint: %s - will recreate all containers", blueprint.Metadata.Name))

		if err := e.reconciler.ForceReconcile(process, blueprint); err != nil {
			errMsg := fmt.Sprintf("Force reconciliation failed for %s: %v", blueprint.Metadata.Name, err)
			e.addProcessLog(process, errMsg)
			result["error"] = errMsg
			return result
		}

		// Collect status after force reconciliation
		status, err := e.reconciler.CollectStatus(blueprint)
		if err != nil {
			errMsg := fmt.Sprintf("Failed to collect status after force reconcile for %s: %v", blueprint.Metadata.Name, err)
			e.addProcessLog(process, errMsg)
			result["error"] = errMsg
			return result
		}

		// Update blueprint status on the server
		if err := e.reconciler.UpdateBlueprintStatus(blueprint, status); err != nil {
			e.addProcessLog(process, fmt.Sprintf("Warning: Failed to update blueprint status for %s: %v", blueprint.Metadata.Name, err))
		}

		e.addProcessLog(process, fmt.Sprintf("Force reconciliation completed for: %s", blueprint.Metadata.Name))
		result["success"] = true
		result["status"] = status
		result["action"] = "force_reconciled"
		return result
	}

	// Check if reconciliation needed
	needsReconciliation, reason := e.checkReconciliationNeeded(blueprint)
	if !needsReconciliation {
		// Collect status even if no reconciliation needed
		status, err := e.reconciler.CollectStatus(blueprint)
		if err != nil {
			errMsg := fmt.Sprintf("Failed to collect status for %s: %v", blueprint.Metadata.Name, err)
			e.addProcessLog(process, errMsg)
			result["error"] = errMsg
			return result
		}

		// Update blueprint status on the server
		if err := e.reconciler.UpdateBlueprintStatus(blueprint, status); err != nil {
			e.addProcessLog(process, fmt.Sprintf("Warning: Failed to update blueprint status for %s: %v", blueprint.Metadata.Name, err))
		}

		e.addProcessLog(process, fmt.Sprintf("Blueprint %s is up to date (replicas: %v)", blueprint.Metadata.Name, status["runningInstances"]))
		result["success"] = true
		result["status"] = status
		result["action"] = "status_updated"
		return result
	}

	log.WithFields(log.Fields{
		"BlueprintName": blueprint.Metadata.Name,
		"Reason":        reason,
	}).Info("Reconciliation needed")
	e.addProcessLog(process, fmt.Sprintf("Reconciliation needed for %s: %s", blueprint.Metadata.Name, reason))

	// Perform reconciliation
	// Note: We pass the parent process for logging, even though we're reconciling multiple blueprints
	// in parallel. The process logs will be interleaved, but that's acceptable.
	if err := e.reconciler.Reconcile(process, blueprint); err != nil {
		errMsg := fmt.Sprintf("Reconciliation failed for %s: %v", blueprint.Metadata.Name, err)
		e.addProcessLog(process, errMsg)
		result["error"] = errMsg
		return result
	}

	// Collect status after reconciliation
	status, err := e.reconciler.CollectStatus(blueprint)
	if err != nil {
		errMsg := fmt.Sprintf("Failed to collect status after reconcile for %s: %v", blueprint.Metadata.Name, err)
		e.addProcessLog(process, errMsg)
		result["error"] = errMsg
		return result
	}

	// Update blueprint status on the server
	if err := e.reconciler.UpdateBlueprintStatus(blueprint, status); err != nil {
		e.addProcessLog(process, fmt.Sprintf("Warning: Failed to update blueprint status for %s: %v", blueprint.Metadata.Name, err))
	}

	e.addProcessLog(process, fmt.Sprintf("Reconciliation completed for: %s", blueprint.Metadata.Name))
	result["success"] = true
	result["status"] = status
	result["action"] = "reconciled"
	return result
}

// handleCleanup processes a cleanup request to remove containers for a deleted blueprint
func (e *Executor) handleCleanup(process *core.Process) {
	log.WithFields(log.Fields{"ProcessID": process.ID}).Info("Handling blueprint cleanup")
	e.addProcessLog(process, "Starting cleanup process")

	// Extract blueprint name from process kwargs
	blueprintName, ok := process.FunctionSpec.KwArgs["blueprintName"].(string)
	if !ok {
		errMsg := "Blueprint name not found in process kwargs"
		e.addProcessLog(process, "Error: "+errMsg)
		e.failProcess(process, errMsg)
		return
	}

	log.WithFields(log.Fields{
		"BlueprintName": blueprintName,
	}).Info("Cleaning up containers for deleted blueprint")
	e.addProcessLog(process, fmt.Sprintf("Cleaning up containers for deleted blueprint: %s", blueprintName))

	// Cleanup containers using the reconciler's cleanup method
	if err := e.reconciler.CleanupDeletedBlueprint(blueprintName); err != nil {
		errMsg := fmt.Sprintf("Cleanup failed for %s: %v", blueprintName, err)
		e.addProcessLog(process, errMsg)
		e.failProcess(process, errMsg)
		return
	}

	e.addProcessLog(process, fmt.Sprintf("Cleanup completed successfully for: %s", blueprintName))

	// Close process successfully
	output := []interface{}{
		map[string]interface{}{
			"status": "cleanup completed",
		},
	}

	if err := e.client.CloseWithOutput(process.ID, output, e.executorPrvKey); err != nil {
		log.WithFields(log.Fields{"Error": err}).Error("Failed to close cleanup process")
		e.addProcessLog(process, fmt.Sprintf("Error closing process: %v", err))
	} else {
		log.WithFields(log.Fields{
			"BlueprintName": blueprintName,
		}).Info("Cleanup completed successfully")
	}
}

// failProcess marks a process as failed with the given reason
func (e *Executor) failProcess(process *core.Process, reason string) {
	log.WithFields(log.Fields{"ProcessID": process.ID, "Reason": reason}).Error("Process failed")
	e.addProcessLog(process, "FAILED: "+reason)
	err := e.client.Fail(process.ID, []string{reason}, e.executorPrvKey)
	if err != nil {
		log.WithFields(log.Fields{"ProcessID": process.ID, "Error": err}).Error("Failed to mark process as failed")
		e.addProcessLog(process, fmt.Sprintf("Error marking process as failed: %v", err))
	}
}
