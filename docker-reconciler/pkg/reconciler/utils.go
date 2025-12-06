package reconciler

import (
	"fmt"
	"time"

	"github.com/colonyos/colonies/pkg/core"
	log "github.com/sirupsen/logrus"
)

// truncateID safely truncates a container ID to the specified length
// If the ID is shorter than the length, returns the full ID
func truncateID(id string, length int) string {
	if len(id) <= length {
		return id
	}
	return id[:length]
}

// addLog adds a log message to both the process and the local log
// If process is nil (e.g., during startup reconciliation), logs to executor-level log
// The message is formatted with a timestamp to match logrus style
// This method is thread-safe and can be called from multiple goroutines
func (r *Reconciler) addLog(process *core.Process, message string) {
	log.Info(message)
	if r.client == nil {
		return
	}

	// Lock to ensure log messages are written atomically and in order
	r.logMu.Lock()
	defer r.logMu.Unlock()

	// Format with timestamp to match logrus style: [timestamp] LEVEL message
	timestamp := time.Now().Format(time.RFC3339)
	formattedMsg := fmt.Sprintf("[%s] INFO\t%s\n", timestamp, message)

	if process != nil {
		// Add to process log
		err := r.client.AddLog(process.ID, formattedMsg, r.executorPrvKey)
		if err != nil {
			log.WithFields(log.Fields{"Error": err}).Debug("Failed to add log to process")
		}
	} else {
		// Add to executor-level log (no process context)
		err := r.client.AddLogToExecutor(r.colonyName, r.executorName, formattedMsg, r.executorPrvKey)
		if err != nil {
			log.WithFields(log.Fields{"Error": err}).Debug("Failed to add executor log")
		}
	}
}

// addErrorLog adds an error log message with ERROR level
func (r *Reconciler) addErrorLog(process *core.Process, message string) {
	log.Error(message)
	if r.client == nil {
		return
	}

	r.logMu.Lock()
	defer r.logMu.Unlock()

	timestamp := time.Now().Format(time.RFC3339)
	formattedMsg := fmt.Sprintf("[%s] ERROR\t%s\n", timestamp, message)

	if process != nil {
		err := r.client.AddLog(process.ID, formattedMsg, r.executorPrvKey)
		if err != nil {
			log.WithFields(log.Fields{"Error": err}).Debug("Failed to add error log to process")
		}
	} else {
		err := r.client.AddLogToExecutor(r.colonyName, r.executorName, formattedMsg, r.executorPrvKey)
		if err != nil {
			log.WithFields(log.Fields{"Error": err}).Debug("Failed to add executor error log")
		}
	}
}

// addWarnLog adds a warning log message with WARN level
func (r *Reconciler) addWarnLog(process *core.Process, message string) {
	log.Warn(message)
	if r.client == nil {
		return
	}

	r.logMu.Lock()
	defer r.logMu.Unlock()

	timestamp := time.Now().Format(time.RFC3339)
	formattedMsg := fmt.Sprintf("[%s] WARN\t%s\n", timestamp, message)

	if process != nil {
		err := r.client.AddLog(process.ID, formattedMsg, r.executorPrvKey)
		if err != nil {
			log.WithFields(log.Fields{"Error": err}).Debug("Failed to add warn log to process")
		}
	} else {
		err := r.client.AddLogToExecutor(r.colonyName, r.executorName, formattedMsg, r.executorPrvKey)
		if err != nil {
			log.WithFields(log.Fields{"Error": err}).Debug("Failed to add executor warn log")
		}
	}
}
