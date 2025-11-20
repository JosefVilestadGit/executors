package executor

import (
	"github.com/colonyos/colonies/pkg/core"
	log "github.com/sirupsen/logrus"
)

// handleReconcile processes a reconciliation request by fetching the blueprint from server
// This is used by cron-based reconciliation where the blueprint name is passed as an argument
func (e *Executor) handleReconcile(process *core.Process) {
	log.WithFields(log.Fields{"ProcessID": process.ID}).Info("Handling blueprint reconciliation")

	// Extract blueprint name from process kwargs
	blueprintName, ok := process.FunctionSpec.KwArgs["blueprintName"].(string)
	if !ok {
		e.failProcess(process, "Blueprint name not found in process kwargs")
		return
	}

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

		// Even though no work is needed, collect and report current status
		// to keep the blueprint status up-to-date
		status, err := e.reconciler.CollectStatus(blueprint)
		if err != nil {
			log.WithFields(log.Fields{"Error": err}).Warn("Failed to collect status")
			// Close without status
			if err := e.client.Close(process.ID, e.executorPrvKey); err != nil {
				log.WithFields(log.Fields{"ProcessID": process.ID, "Error": err}).Error("Failed to close process")
			}
			return
		}

		// Close with status to update blueprint status field
		// Note: We don't include metadata.lastReconciliationProcess anymore
		// as it's now only updated by the cron controller when creating the process
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
			}).Info("Status updated successfully (no reconciliation needed)")
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
		// Close without status
		if err := e.client.Close(process.ID, e.executorPrvKey); err != nil {
			log.WithFields(log.Fields{"ProcessID": process.ID, "Error": err}).Error("Failed to close process")
		}
		return
	}

	// Close with status output
	// Note: We don't include metadata.lastReconciliationProcess anymore
	// as it's now only updated by the cron controller when creating the process
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
