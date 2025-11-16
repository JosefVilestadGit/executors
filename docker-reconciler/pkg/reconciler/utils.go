package reconciler

import (
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
func (r *Reconciler) addLog(process *core.Process, message string) {
	log.Info(message)
	if r.client != nil {
		err := r.client.AddLog(process.ID, message+"\n", r.executorPrvKey)
		if err != nil {
			log.WithFields(log.Fields{"Error": err}).Error("Failed to add log to process")
		}
	}
}
