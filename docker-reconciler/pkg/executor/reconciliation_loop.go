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

	// Get blueprints filtered by location for both kinds
	var myBlueprints []*core.Blueprint

	// Fetch ExecutorDeployment blueprints for this location
	executorBlueprints, err := e.client.GetBlueprintsByLocation(e.colonyName, "ExecutorDeployment", e.location, e.colonyPrvKey)
	if err != nil {
		log.WithFields(log.Fields{"Error": err, "Kind": "ExecutorDeployment"}).Error("Failed to fetch blueprints for startup reconciliation")
	} else {
		myBlueprints = append(myBlueprints, executorBlueprints...)
	}

	// Fetch DockerDeployment blueprints for this location
	dockerBlueprints, err := e.client.GetBlueprintsByLocation(e.colonyName, "DockerDeployment", e.location, e.colonyPrvKey)
	if err != nil {
		log.WithFields(log.Fields{"Error": err, "Kind": "DockerDeployment"}).Error("Failed to fetch blueprints for startup reconciliation")
	} else {
		myBlueprints = append(myBlueprints, dockerBlueprints...)
	}

	if len(myBlueprints) == 0 {
		log.WithFields(log.Fields{"Location": e.location}).Info("No blueprints found for this location on startup reconciliation")
		return
	}

	log.WithFields(log.Fields{
		"Count":    len(myBlueprints),
		"Location": e.location,
	}).Info("Starting reconciliation of blueprints for this location")

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
