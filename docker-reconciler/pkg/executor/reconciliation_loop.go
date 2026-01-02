package executor

import (
	"errors"
	"time"

	"github.com/colonyos/colonies/pkg/core"
	log "github.com/sirupsen/logrus"
)

// ServeForEver runs the main executor loop, continuously polling for reconciliation processes
// The reconciler is now completely stateless and driven by server-side crons
func (e *Executor) ServeForEver() error {
	log.Info("Starting reconciler in stateless mode (cron-driven)")

	// Perform startup reconciliation of all blueprints
	e.performStartupReconciliation()

	// Main process assignment loop
	for {
		process, err := e.assignNextProcess()
		if err != nil {
			// Check if this is just a "no processes available" error
			var coloniesError *core.ColoniesError
			if errors.As(err, &coloniesError) {
				if coloniesError.Status == 404 { // No processes can be selected for executor
					continue
				}
			}

			// For other errors, log and retry
			log.WithFields(log.Fields{"Error": err}).Error("Failed to assign process to executor")
			log.Error("Retrying in 5 seconds ...")
			time.Sleep(5 * time.Second)
			continue
		}

		// Dispatch the process to the appropriate handler
		e.dispatchProcess(process)
	}
}

// assignNextProcess polls the Colonies server for the next available process
func (e *Executor) assignNextProcess() (*core.Process, error) {
	process, err := e.client.AssignWithContext(e.colonyName, 100, e.ctx, "", "", e.executorPrvKey)
	if err != nil {
		return nil, err
	}

	log.WithFields(log.Fields{
		"ProcessID":    process.ID,
		"ExecutorID":   e.executorID,
		"ExecutorName": e.executorName,
		"FuncName":     process.FunctionSpec.FuncName,
	}).Info("Assigned process to executor")

	return process, nil
}

// dispatchProcess routes a process to the appropriate handler based on function name
func (e *Executor) dispatchProcess(process *core.Process) {
	switch process.FunctionSpec.FuncName {
	case "reconcile":
		e.handleReconcile(process)
	case "cleanup":
		e.handleCleanup(process)
	default:
		e.handleUnsupportedFunction(process)
	}
}

// handleUnsupportedFunction handles processes with unsupported function names
func (e *Executor) handleUnsupportedFunction(process *core.Process) {
	log.WithFields(log.Fields{"FuncName": process.FunctionSpec.FuncName}).Error("Unsupported funcname")
	err := e.client.Fail(process.ID, []string{"Unsupported funcname"}, e.executorPrvKey)
	if err != nil {
		log.WithFields(log.Fields{"ProcessId": process.ID, "Error": err}).Error("Failed to close process as failed")
	}
}

// performStartupReconciliation reconciles all blueprints on startup
func (e *Executor) performStartupReconciliation() {
	log.Info("Performing startup reconciliation of all blueprints")

	// Get all blueprints in the colony
	blueprints, err := e.client.GetBlueprints(e.colonyName, "", e.colonyPrvKey)
	if err != nil {
		log.WithFields(log.Fields{"Error": err}).Error("Failed to fetch blueprints for startup reconciliation")
		return
	}

	if len(blueprints) == 0 {
		log.Info("No blueprints found for startup reconciliation")
		return
	}

	// All blueprints are handled - filtering by location is done by GetBlueprints API
	myBlueprints := blueprints

	if len(myBlueprints) == 0 {
		log.WithFields(log.Fields{
			"TotalBlueprints": len(blueprints),
			"ExecutorName":    e.executorName,
		}).Info("No blueprints assigned to this executor for startup reconciliation")
		return
	}

	log.WithFields(log.Fields{"Count": len(myBlueprints), "Total": len(blueprints)}).Info("Starting reconciliation of assigned blueprints")

	// Reconcile each blueprint
	reconciledCount := 0
	for _, blueprint := range myBlueprints {
		log.WithFields(log.Fields{
			"BlueprintName": blueprint.Metadata.Name,
			"Kind":          blueprint.Kind,
		}).Info("Reconciling blueprint on startup")

		// Check if reconciliation is needed
		needsReconciliation, reason := e.checkReconciliationNeeded(blueprint)
		if !needsReconciliation {
			// Just collect and update status
			status, err := e.reconciler.CollectStatus(blueprint)
			if err != nil {
				log.WithFields(log.Fields{
					"BlueprintName": blueprint.Metadata.Name,
					"Error":         err,
				}).Warning("Failed to collect status on startup")
			} else {
				// Update blueprint status on the server
				if err := e.reconciler.UpdateBlueprintStatus(blueprint, status); err != nil {
					log.WithFields(log.Fields{
						"BlueprintName": blueprint.Metadata.Name,
						"Error":         err,
					}).Warning("Failed to update blueprint status on startup")
				}
				log.WithFields(log.Fields{
					"BlueprintName": blueprint.Metadata.Name,
					"Status":        status,
				}).Debug("Blueprint status updated on startup")
			}
			continue
		}

		log.WithFields(log.Fields{
			"BlueprintName": blueprint.Metadata.Name,
			"Reason":        reason,
		}).Info("Reconciliation needed on startup")

		// Perform reconciliation without a process (startup mode)
		if err := e.reconciler.Reconcile(nil, blueprint); err != nil {
			log.WithFields(log.Fields{
				"BlueprintName": blueprint.Metadata.Name,
				"Error":         err,
			}).Error("Startup reconciliation failed")
		} else {
			// Collect and update status after successful reconciliation
			status, err := e.reconciler.CollectStatus(blueprint)
			if err == nil {
				if err := e.reconciler.UpdateBlueprintStatus(blueprint, status); err != nil {
					log.WithFields(log.Fields{
						"BlueprintName": blueprint.Metadata.Name,
						"Error":         err,
					}).Warning("Failed to update blueprint status after startup reconciliation")
				}
			}
			reconciledCount++
			log.WithFields(log.Fields{
				"BlueprintName": blueprint.Metadata.Name,
			}).Info("Startup reconciliation completed")
		}
	}

	log.WithFields(log.Fields{
		"Total":       len(myBlueprints),
		"Reconciled":  reconciledCount,
		"UpToDate":    len(myBlueprints) - reconciledCount,
	}).Info("Startup reconciliation completed")
}

// shouldHandleBlueprint returns true if this executor should handle the given blueprint
// Process routing and GetBlueprintsByLocation already filter by executor type and location
// This function is kept for any future filtering needs but currently always returns true
func (e *Executor) shouldHandleBlueprint(blueprint *core.Blueprint) bool {
	return true
}
