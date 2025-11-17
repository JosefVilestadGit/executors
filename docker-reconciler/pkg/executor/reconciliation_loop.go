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
