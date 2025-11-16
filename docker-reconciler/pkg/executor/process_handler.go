package executor

import (
	"github.com/colonyos/colonies/pkg/core"
	log "github.com/sirupsen/logrus"
)

// Note: reconcileStartupState is now in startup_reconciliation.go as performStartupReconciliation

// handleReconcile processes a reconciliation request
func (e *Executor) handleReconcile(process *core.Process) {
	log.WithFields(log.Fields{"ProcessID": process.ID}).Info("Handling reconciliation")

	// Get the reconciliation data from the process FunctionSpec
	if process.FunctionSpec.Reconciliation == nil {
		e.failProcess(process, "No reconciliation data found in process FunctionSpec")
		return
	}

	reconciliation := process.FunctionSpec.Reconciliation

	// For deployment-controller, we work with the "New" blueprint (the desired state)
	// The reconciliation.New contains the current/desired blueprint state
	// The reconciliation.Old contains the previous state (nil for create, set for update)
	var blueprint *core.Blueprint
	if reconciliation.New != nil {
		blueprint = reconciliation.New
	} else if reconciliation.Old != nil {
		// For delete operations, Old is set and New is nil
		blueprint = reconciliation.Old
	} else {
		e.failProcess(process, "No blueprint found in reconciliation data")
		return
	}

	log.WithFields(log.Fields{
		"ResourceName": blueprint.Metadata.Name,
		"ResourceKind": blueprint.Kind,
		"Action":       reconciliation.Action,
	}).Info("Processing blueprint reconciliation")

	// Perform reconciliation
	if err := e.reconciler.Reconcile(process, blueprint); err != nil {
		e.failProcess(process, "Reconciliation failed: "+err.Error())
		return
	}

	// Track or untrack the blueprint based on the action
	if reconciliation.Action == "delete" {
		// Remove from managed blueprints
		e.resourcesMutex.Lock()
		delete(e.managedResources, blueprint.ID)
		e.resourcesMutex.Unlock()
		log.WithFields(log.Fields{"ResourceID": blueprint.ID, "ResourceName": blueprint.Metadata.Name}).Info("Removed blueprint from managed blueprints")
	} else {
		// Add/update in managed blueprints (for create and update actions)
		e.resourcesMutex.Lock()
		e.managedResources[blueprint.ID] = blueprint
		e.resourcesMutex.Unlock()
		log.WithFields(log.Fields{"ResourceID": blueprint.ID, "ResourceName": blueprint.Metadata.Name}).Info("Added/updated blueprint in managed blueprints")
	}

	// Collect status after successful reconciliation
	status, err := e.reconciler.CollectStatus(blueprint)
	if err != nil {
		log.WithFields(log.Fields{"Error": err, "ResourceName": blueprint.Metadata.Name}).Warn("Failed to collect blueprint status")
		// Don't fail the process - reconciliation succeeded, status collection is best-effort
		// Close without status output
		if err := e.client.Close(process.ID, e.executorPrvKey); err != nil {
			log.WithFields(log.Fields{"ProcessID": process.ID, "Error": err}).Error("Failed to close process")
		} else {
			log.WithFields(log.Fields{"ProcessID": process.ID}).Info("Process completed successfully")
		}
	} else {
		// Close the process with status output so the server can update the blueprint
		output := []interface{}{
			map[string]interface{}{
				"status": status,
			},
		}
		if err := e.client.CloseWithOutput(process.ID, output, e.executorPrvKey); err != nil {
			log.WithFields(log.Fields{"ProcessID": process.ID, "Error": err}).Error("Failed to close process with output")
		} else {
			log.WithFields(log.Fields{
				"ProcessID":      process.ID,
				"ResourceName":   blueprint.Metadata.Name,
				"TotalInstances": status["totalInstances"],
			}).Info("Process completed successfully with status update")
		}
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
