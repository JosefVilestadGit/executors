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
// If process is nil (e.g., during startup reconciliation), only logs locally
// The message is formatted with a timestamp to match logrus style
// This method is thread-safe and can be called from multiple goroutines
func (r *Reconciler) addLog(process *core.Process, message string) {
	log.Info(message)
	if r.client != nil && process != nil {
		// Lock to ensure log messages are written atomically and in order
		r.logMu.Lock()
		defer r.logMu.Unlock()

		// Format with timestamp to match logrus style: time="2006-01-02T15:04:05Z07:00" level=info msg="message"
		timestamp := time.Now().Format(time.RFC3339)
		formattedMsg := fmt.Sprintf("time=\"%s\" level=info msg=\"%s\"\n", timestamp, message)
		err := r.client.AddLog(process.ID, formattedMsg, r.executorPrvKey)
		if err != nil {
			log.WithFields(log.Fields{"Error": err}).Error("Failed to add log to process")
		}
	}
}
