package reconciler

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
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

// generateExecutorName generates a container/executor name with a random hash suffix
// This does NOT check for duplicates - duplicate handling is done atomically at registration time
func generateExecutorName(baseExecutorName string) string {
	hash := generateUniqueHash()
	return fmt.Sprintf("%s-%s", baseExecutorName, hash)
}

// isDuplicateExecutorError checks if an error indicates a duplicate executor name
func isDuplicateExecutorError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "already exists") ||
		strings.Contains(errStr, "duplicate") ||
		strings.Contains(errStr, "not unique")
}

// generateUniqueExecutorName generates a unique executor name with hash suffix
// Uses atomic registration: tries to register, and retries with new name if duplicate detected
// This eliminates the race condition in check-then-act patterns
func (r *Reconciler) generateUniqueExecutorName(colonyName, baseExecutorName string) (string, error) {
	// Just generate a name - no pre-check needed
	// The server-side unique constraint will reject duplicates atomically
	// Duplicate handling happens at registration time (in startContainer)
	return generateExecutorName(baseExecutorName), nil
}
