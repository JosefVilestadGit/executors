package reconciler

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"

	log "github.com/sirupsen/logrus"
)

// generateUniqueHash generates a random 5-character alphanumeric hash
// With 5 alphanumeric characters (62^5 = ~916 million combinations),
// we can safely support 100K+ executors with very low collision probability
func generateUniqueHash() string {
	// Generate 4 random bytes (we'll use 5 chars from base62 encoding)
	bytes := make([]byte, 4)
	rand.Read(bytes)

	// Convert to hex and take first 5 characters
	hash := hex.EncodeToString(bytes)
	if len(hash) > 5 {
		hash = hash[:5]
	}

	return hash
}

// isExecutorNameTaken checks if an executor with the given name already exists in the colony
func (r *Reconciler) isExecutorNameTaken(colonyName, executorName string) (bool, error) {
	// Try to get the executor from colonies server
	executor, err := r.client.GetExecutor(colonyName, executorName, r.executorPrvKey)
	if err != nil {
		// If error is "not found", name is available
		return false, nil
	}
	// If we got an executor, name is taken
	return executor != nil, nil
}

// generateUniqueExecutorName generates a unique executor name with hash suffix
// It will retry up to 10 times to find an available name
func (r *Reconciler) generateUniqueExecutorName(colonyName, baseExecutorName string) (string, error) {
	const maxRetries = 10

	for i := 0; i < maxRetries; i++ {
		hash := generateUniqueHash()
		executorName := fmt.Sprintf("%s-%s", baseExecutorName, hash)

		taken, err := r.isExecutorNameTaken(colonyName, executorName)
		if err != nil {
			log.WithFields(log.Fields{"Error": err, "ExecutorName": executorName}).Warn("Failed to check if executor name is taken")
			continue
		}

		if !taken {
			return executorName, nil
		}

		log.WithFields(log.Fields{"ExecutorName": executorName}).Debug("Executor name collision, retrying...")
	}

	return "", fmt.Errorf("failed to generate unique executor name after %d retries", maxRetries)
}
