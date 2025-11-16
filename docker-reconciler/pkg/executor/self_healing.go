package executor

import (
	"time"

	"github.com/colonyos/colonies/pkg/core"
	log "github.com/sirupsen/logrus"
)

// SelfHealingConfig defines configuration for the self-healing loop
type SelfHealingConfig struct {
	Enabled       bool          // Whether self-healing is enabled
	CheckInterval time.Duration // How often to check for drift
}

// DefaultSelfHealingConfig returns the default self-healing configuration
func DefaultSelfHealingConfig() SelfHealingConfig {
	return SelfHealingConfig{
		Enabled:       false, // Disabled by default (can be enabled via environment variable)
		CheckInterval: 60 * time.Second,
	}
}

// startSelfHealingLoop runs a background loop that periodically checks for state drift
// and triggers reconciliation when the actual state diverges from desired state
//
// This provides automatic recovery from:
// - Manual container deletions
// - Container crashes
// - Configuration drift
// - Network/system failures
//
// Note: This is currently disabled by default. Enable via ENABLE_SELF_HEALING env var.
func (e *Executor) startSelfHealingLoop(config SelfHealingConfig) {
	if !config.Enabled {
		log.Info("Self-healing loop disabled")
		return
	}

	log.WithFields(log.Fields{
		"CheckInterval": config.CheckInterval,
	}).Info("Starting self-healing background loop")

	ticker := time.NewTicker(config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-e.ctx.Done():
			log.Info("Self-healing loop stopping due to context cancellation")
			return
		case <-ticker.C:
			e.performSelfHealingCheck()
		}
	}
}

// performSelfHealingCheck checks all managed blueprints for drift and triggers reconciliation if needed
func (e *Executor) performSelfHealingCheck() {
	log.Debug("Performing self-healing drift check")

	e.resourcesMutex.RLock()
	blueprints := make([]*core.Blueprint, 0, len(e.managedResources))
	for _, blueprint := range e.managedResources {
		blueprints = append(blueprints, blueprint)
	}
	e.resourcesMutex.RUnlock()

	if len(blueprints) == 0 {
		log.Debug("No managed blueprints to check")
		return
	}

	driftsDetected := 0
	for _, blueprint := range blueprints {
		if e.detectAndFixDrift(blueprint) {
			driftsDetected++
		}
	}

	if driftsDetected > 0 {
		log.WithFields(log.Fields{
			"DriftsDetected": driftsDetected,
			"TotalBlueprints": len(blueprints),
		}).Info("Self-healing: drift detected and reconciliation triggered")
	} else {
		log.Debug("Self-healing: no drift detected")
	}
}

// detectAndFixDrift checks a single blueprint for drift and fixes it if found
// Returns true if drift was detected and fixed
func (e *Executor) detectAndFixDrift(blueprint *core.Blueprint) bool {
	driftDetected := false

	// 1. Check and clean up old generation containers
	hasOldGeneration, err := e.reconciler.HasOldGenerationContainers(blueprint)
	if err != nil {
		log.WithFields(log.Fields{
			"Error":         err,
			"BlueprintName": blueprint.Metadata.Name,
		}).Warn("Self-healing: failed to check for old generation containers")
	} else if hasOldGeneration {
		log.WithFields(log.Fields{
			"BlueprintName": blueprint.Metadata.Name,
		}).Info("Self-healing: removing old generation containers")

		if err := e.reconciler.CleanupOldGenerationContainers(blueprint); err != nil {
			log.WithFields(log.Fields{
				"Error":         err,
				"BlueprintName": blueprint.Metadata.Name,
			}).Error("Self-healing: failed to cleanup old generation containers")
		} else {
			driftDetected = true
		}
	}

	// 2. Check and adjust replica count
	status, err := e.reconciler.CollectStatus(blueprint)
	if err != nil {
		log.WithFields(log.Fields{
			"Error":         err,
			"BlueprintName": blueprint.Metadata.Name,
		}).Warn("Self-healing: failed to collect status")
		return driftDetected
	}

	runningInstances, ok := status["runningInstances"].(int)
	if !ok {
		return driftDetected
	}

	desiredReplicas := e.getDesiredReplicas(blueprint)
	if desiredReplicas < 0 {
		return driftDetected
	}

	if runningInstances != desiredReplicas {
		log.WithFields(log.Fields{
			"BlueprintName": blueprint.Metadata.Name,
			"Running":       runningInstances,
			"Desired":       desiredReplicas,
		}).Info("Self-healing: adjusting replica count")

		if err := e.reconciler.AdjustReplicas(blueprint); err != nil {
			log.WithFields(log.Fields{
				"Error":         err,
				"BlueprintName": blueprint.Metadata.Name,
			}).Error("Self-healing: failed to adjust replicas")
		} else {
			driftDetected = true
		}
	}

	// 3. Clean up stopped containers
	if err := e.reconciler.CleanupStoppedContainers(); err != nil {
		log.WithFields(log.Fields{
			"Error": err,
		}).Warn("Self-healing: failed to cleanup stopped containers")
	}

	return driftDetected
}
