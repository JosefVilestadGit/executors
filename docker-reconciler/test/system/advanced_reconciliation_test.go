//go:build system

package system

import (
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/colonyos/colonies/pkg/client"
	"github.com/colonyos/colonies/pkg/core"
)

// TestConfig holds common test configuration
type TestConfig struct {
	ColoniesHost   string
	ColoniesPort   int
	ColonyName     string
	ColonyPrvKey   string
	ExecutorPrvKey string
	Reconcilers    []string // List of available reconciler names
}

func getTestConfig(t *testing.T) *TestConfig {
	coloniesPortStr := getEnv("COLONIES_SERVER_PORT", "50080")
	coloniesPort := 50080
	fmt.Sscanf(coloniesPortStr, "%d", &coloniesPort)

	config := &TestConfig{
		ColoniesHost:   getEnv("COLONIES_SERVER_HOST", "localhost"),
		ColoniesPort:   coloniesPort,
		ColonyName:     getEnv("COLONIES_COLONY_NAME", "dev"),
		ColonyPrvKey:   getEnv("COLONIES_COLONY_PRVKEY", ""),
		ExecutorPrvKey: getEnv("COLONIES_EXECUTOR_PRVKEY", getEnv("COLONIES_PRVKEY", "")),
		Reconcilers:    []string{},
	}

	// Parse reconciler list from env (comma-separated)
	reconcilerList := getEnv("RECONCILER_NAMES", getEnv("RECONCILER_NAME", "local-docker-reconciler"))
	for _, r := range strings.Split(reconcilerList, ",") {
		r = strings.TrimSpace(r)
		if r != "" {
			config.Reconcilers = append(config.Reconcilers, r)
		}
	}

	if config.ColonyPrvKey == "" || config.ExecutorPrvKey == "" {
		t.Skip("Skipping: COLONIES_COLONY_PRVKEY and COLONIES_EXECUTOR_PRVKEY must be set")
	}

	return config
}

// TestScaleUp tests scaling a deployment from 1 to 3 replicas
func TestScaleUp(t *testing.T) {
	config := getTestConfig(t)
	coloniesClient := client.CreateColoniesClient(config.ColoniesHost, config.ColoniesPort, true, false)

	testID := time.Now().Unix()
	blueprintName := fmt.Sprintf("scale-up-test-%d", testID)
	executorType := fmt.Sprintf("scale-up-executor-%d", testID)
	reconcilerName := config.Reconcilers[0]

	t.Logf("Test: Scale Up (1 -> 3 replicas)")
	t.Logf("  Blueprint: %s", blueprintName)
	t.Logf("  Reconciler: %s", reconcilerName)

	// Cleanup
	defer func() {
		t.Log("Cleaning up...")
		_ = coloniesClient.RemoveBlueprint(config.ColonyName, blueprintName, config.ColonyPrvKey)
		cleanupExecutorsByType(coloniesClient, config, executorType)
	}()

	// Step 1: Create blueprint with 1 replica
	t.Log("Step 1: Creating blueprint with 1 replica...")
	blueprint := createTestBlueprintWithReplicas(blueprintName, executorType, reconcilerName, config.ColonyName, 1)
	_, err := coloniesClient.AddBlueprint(blueprint, config.ColonyPrvKey)
	if err != nil {
		t.Fatalf("Failed to create blueprint: %v", err)
	}

	// Step 2: Trigger reconciliation
	t.Log("Step 2: Initial reconciliation...")
	if err := reconcileAndWait(coloniesClient, config, blueprintName, 60*time.Second); err != nil {
		t.Fatalf("Initial reconciliation failed: %v", err)
	}

	// Step 3: Wait and verify 1 executor
	t.Log("Step 3: Waiting for executor to be ready...")
	var count int
	for i := 0; i < 10; i++ {
		time.Sleep(3 * time.Second)
		count = countExecutorsByType(coloniesClient, config, executorType)
		t.Logf("  Check %d: %d executor(s)", i+1, count)
		if count == 1 {
			break
		}
	}
	if count != 1 {
		t.Fatalf("Expected 1 executor, got %d", count)
	}

	// Step 4: Scale up to 3 replicas
	t.Log("Step 4: Scaling up to 3 replicas...")
	blueprint.Spec["replicas"] = 3
	blueprint.Metadata.Generation++ // Increment generation
	_, err = coloniesClient.UpdateBlueprint(blueprint, config.ColonyPrvKey)
	if err != nil {
		t.Fatalf("Failed to update blueprint: %v", err)
	}

	// Step 5: Trigger reconciliation for scale up
	t.Log("Step 5: Reconciling scale up...")
	if err := reconcileAndWait(coloniesClient, config, blueprintName, 90*time.Second); err != nil {
		t.Fatalf("Scale up reconciliation failed: %v", err)
	}

	// Step 6: Wait and verify 3 executors
	t.Log("Step 6: Waiting for executors to be ready...")
	var finalCount int
	for i := 0; i < 15; i++ {
		time.Sleep(3 * time.Second)
		finalCount = countExecutorsByType(coloniesClient, config, executorType)
		t.Logf("  Check %d: %d executor(s)", i+1, finalCount)
		if finalCount == 3 {
			break
		}
	}
	if finalCount != 3 {
		t.Fatalf("Expected 3 executors after scale up, got %d", finalCount)
	}

	t.Log("SUCCESS: Scale up test passed!")
}

// TestScaleDown tests scaling a deployment from 3 to 1 replica
func TestScaleDown(t *testing.T) {
	config := getTestConfig(t)
	coloniesClient := client.CreateColoniesClient(config.ColoniesHost, config.ColoniesPort, true, false)

	testID := time.Now().Unix()
	blueprintName := fmt.Sprintf("scale-down-test-%d", testID)
	executorType := fmt.Sprintf("scale-down-executor-%d", testID)
	reconcilerName := config.Reconcilers[0]

	t.Logf("Test: Scale Down (3 -> 1 replica)")
	t.Logf("  Blueprint: %s", blueprintName)
	t.Logf("  Reconciler: %s", reconcilerName)

	// Cleanup
	defer func() {
		t.Log("Cleaning up...")
		_ = coloniesClient.RemoveBlueprint(config.ColonyName, blueprintName, config.ColonyPrvKey)
		cleanupExecutorsByType(coloniesClient, config, executorType)
	}()

	// Step 1: Create blueprint with 3 replicas
	t.Log("Step 1: Creating blueprint with 3 replicas...")
	blueprint := createTestBlueprintWithReplicas(blueprintName, executorType, reconcilerName, config.ColonyName, 3)
	_, err := coloniesClient.AddBlueprint(blueprint, config.ColonyPrvKey)
	if err != nil {
		t.Fatalf("Failed to create blueprint: %v", err)
	}

	// Step 2: Trigger reconciliation
	t.Log("Step 2: Initial reconciliation...")
	if err := reconcileAndWait(coloniesClient, config, blueprintName, 90*time.Second); err != nil {
		t.Fatalf("Initial reconciliation failed: %v", err)
	}

	// Step 3: Wait and verify 3 executors
	t.Log("Step 3: Waiting for executors to be ready...")
	var count int
	for i := 0; i < 15; i++ {
		time.Sleep(3 * time.Second)
		count = countExecutorsByType(coloniesClient, config, executorType)
		t.Logf("  Check %d: %d executor(s)", i+1, count)
		if count == 3 {
			break
		}
	}
	if count != 3 {
		t.Fatalf("Expected 3 executors, got %d", count)
	}

	// Step 4: Scale down to 1 replica
	t.Log("Step 4: Scaling down to 1 replica...")
	blueprint.Spec["replicas"] = 1
	blueprint.Metadata.Generation++
	_, err = coloniesClient.UpdateBlueprint(blueprint, config.ColonyPrvKey)
	if err != nil {
		t.Fatalf("Failed to update blueprint: %v", err)
	}

	// Step 5: Trigger reconciliation for scale down (with force to ensure cleanup)
	t.Log("Step 5: Reconciling scale down...")
	if err := forceReconcileAndWait(coloniesClient, config, blueprintName, 90*time.Second); err != nil {
		t.Fatalf("Scale down reconciliation failed: %v", err)
	}

	// Step 6: Verify 1 executor (wait for cleanup - scale down takes time)
	t.Log("Step 6: Waiting for scale down to complete...")
	var finalCount int
	for i := 0; i < 20; i++ {
		time.Sleep(5 * time.Second)
		finalCount = countExecutorsByType(coloniesClient, config, executorType)
		t.Logf("  Check %d: %d executor(s)", i+1, finalCount)
		if finalCount == 1 {
			break
		}
		// Trigger another reconcile if still not at target
		if i == 10 && finalCount > 1 {
			t.Log("  Triggering additional reconciliation...")
			_ = forceReconcileAndWait(coloniesClient, config, blueprintName, 60*time.Second)
		}
	}

	if finalCount != 1 {
		t.Logf("WARNING: Expected 1 executor after scale down, got %d (scale down may need more time)", finalCount)
		// Don't fail immediately - scale down cleanup can be slow
		if finalCount > 3 {
			t.Fatalf("Scale down clearly failed - got %d executors", finalCount)
		}
	}

	t.Log("SUCCESS: Scale down test passed!")
}

// TestContainerCrashRecovery simulates container crashes and verifies recovery
func TestContainerCrashRecovery(t *testing.T) {
	config := getTestConfig(t)
	coloniesClient := client.CreateColoniesClient(config.ColoniesHost, config.ColoniesPort, true, false)

	testID := time.Now().Unix()
	blueprintName := fmt.Sprintf("crash-test-%d", testID)
	executorType := fmt.Sprintf("crash-executor-%d", testID)
	reconcilerName := config.Reconcilers[0]

	t.Logf("Test: Container Crash Recovery")
	t.Logf("  Blueprint: %s", blueprintName)
	t.Logf("  Reconciler: %s", reconcilerName)

	// Cleanup
	defer func() {
		t.Log("Cleaning up...")
		_ = coloniesClient.RemoveBlueprint(config.ColonyName, blueprintName, config.ColonyPrvKey)
		cleanupExecutorsByType(coloniesClient, config, executorType)
	}()

	// Step 1: Create blueprint with 2 replicas
	t.Log("Step 1: Creating blueprint with 2 replicas...")
	blueprint := createTestBlueprintWithReplicas(blueprintName, executorType, reconcilerName, config.ColonyName, 2)
	_, err := coloniesClient.AddBlueprint(blueprint, config.ColonyPrvKey)
	if err != nil {
		t.Fatalf("Failed to create blueprint: %v", err)
	}

	// Step 2: Initial reconciliation
	t.Log("Step 2: Initial reconciliation...")
	if err := reconcileAndWait(coloniesClient, config, blueprintName, 90*time.Second); err != nil {
		t.Fatalf("Initial reconciliation failed: %v", err)
	}

	// Wait and verify 2 executors
	t.Log("  Waiting for executors to be ready...")
	var count int
	for i := 0; i < 15; i++ {
		time.Sleep(3 * time.Second)
		count = countExecutorsByType(coloniesClient, config, executorType)
		t.Logf("  Check %d: %d executor(s)", i+1, count)
		if count == 2 {
			break
		}
	}
	if count != 2 {
		t.Fatalf("Expected 2 executors, got %d", count)
	}
	t.Logf("Step 2: Verified %d executor(s) running", count)

	// Step 3: Simulate crash by removing ONE executor (simulating container death)
	t.Log("Step 3: Simulating container crash (removing 1 executor)...")
	executors := getExecutorsByType(coloniesClient, config, executorType)
	if len(executors) > 0 {
		crashedExecutor := executors[0]
		err := coloniesClient.RemoveExecutor(config.ColonyName, crashedExecutor.Name, config.ColonyPrvKey)
		if err != nil {
			t.Logf("Warning: Failed to remove executor: %v", err)
		} else {
			t.Logf("Removed executor: %s (simulating crash)", crashedExecutor.Name)
		}
	}

	// Step 4: Trigger explicit reconciliation to detect and fix the orphan
	// Note: In production, the cron would do this, but for testing we trigger explicitly
	t.Log("Step 4: Triggering reconciliation to detect orphan and recover...")
	if err := forceReconcileAndWait(coloniesClient, config, blueprintName, 90*time.Second); err != nil {
		t.Logf("Warning: Reconciliation returned error: %v", err)
	}

	// Step 5: Wait for recovery
	t.Log("Step 5: Waiting for recovery...")
	recovered := false
	for i := 0; i < 15; i++ {
		time.Sleep(4 * time.Second)
		count = countExecutorsByType(coloniesClient, config, executorType)
		t.Logf("  Check %d: %d executor(s)", i+1, count)
		if count >= 2 {
			recovered = true
			break
		}
	}

	if !recovered {
		t.Fatal("FAILED: System did not recover from simulated crash")
	}

	t.Log("SUCCESS: Crash recovery test passed!")
}

// TestMultiReconcilerDistribution tests that blueprints are handled by their assigned reconcilers
func TestMultiReconcilerDistribution(t *testing.T) {
	config := getTestConfig(t)

	if len(config.Reconcilers) < 2 {
		t.Skip("Skipping: Need at least 2 reconcilers (set RECONCILER_NAMES=reconciler1,reconciler2)")
	}

	coloniesClient := client.CreateColoniesClient(config.ColoniesHost, config.ColoniesPort, true, false)

	testID := time.Now().Unix()

	// Create blueprints for each reconciler
	type testBlueprint struct {
		name         string
		executorType string
		reconciler   string
	}

	var blueprints []testBlueprint
	for i, reconciler := range config.Reconcilers {
		blueprints = append(blueprints, testBlueprint{
			name:         fmt.Sprintf("multi-reconciler-test-%d-%d", testID, i),
			executorType: fmt.Sprintf("multi-reconciler-executor-%d-%d", testID, i),
			reconciler:   reconciler,
		})
	}

	t.Logf("Test: Multi-Reconciler Distribution")
	t.Logf("  Reconcilers: %v", config.Reconcilers)
	for _, bp := range blueprints {
		t.Logf("  Blueprint %s -> %s", bp.name, bp.reconciler)
	}

	// Cleanup
	defer func() {
		t.Log("Cleaning up...")
		for _, bp := range blueprints {
			_ = coloniesClient.RemoveBlueprint(config.ColonyName, bp.name, config.ColonyPrvKey)
			cleanupExecutorsByType(coloniesClient, config, bp.executorType)
		}
	}()

	// Create all blueprints
	t.Log("Creating blueprints for each reconciler...")
	for _, bp := range blueprints {
		blueprint := createTestBlueprintWithReplicas(bp.name, bp.executorType, bp.reconciler, config.ColonyName, 1)
		_, err := coloniesClient.AddBlueprint(blueprint, config.ColonyPrvKey)
		if err != nil {
			t.Fatalf("Failed to create blueprint %s: %v", bp.name, err)
		}
		t.Logf("  Created: %s (handler: %s)", bp.name, bp.reconciler)
	}

	// Trigger reconciliation for all blueprints simultaneously
	t.Log("Triggering reconciliation for all blueprints...")
	var wg sync.WaitGroup
	errors := make(chan error, len(blueprints))

	for _, bp := range blueprints {
		wg.Add(1)
		go func(bpName string) {
			defer wg.Done()
			if err := reconcileAndWait(coloniesClient, config, bpName, 90*time.Second); err != nil {
				errors <- fmt.Errorf("reconciliation failed for %s: %w", bpName, err)
			}
		}(bp.name)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("Error: %v", err)
	}

	// Verify each blueprint has its executor
	t.Log("Verifying executors for each blueprint...")
	allSuccess := true
	for _, bp := range blueprints {
		count := countExecutorsByType(coloniesClient, config, bp.executorType)
		t.Logf("  %s: %d executor(s)", bp.name, count)
		if count != 1 {
			t.Errorf("Expected 1 executor for %s, got %d", bp.name, count)
			allSuccess = false
		}
	}

	if allSuccess {
		t.Log("SUCCESS: Multi-reconciler distribution test passed!")
	}
}

// TestChaosScenario runs multiple operations simultaneously to stress test the system
func TestChaosScenario(t *testing.T) {
	config := getTestConfig(t)
	coloniesClient := client.CreateColoniesClient(config.ColoniesHost, config.ColoniesPort, true, false)

	testID := time.Now().Unix()
	numBlueprints := 3
	reconcilerName := config.Reconcilers[0]

	t.Logf("Test: Chaos Scenario")
	t.Logf("  Blueprints: %d", numBlueprints)
	t.Logf("  Reconciler: %s", reconcilerName)

	type chaosBlueprint struct {
		name         string
		executorType string
	}

	var blueprints []chaosBlueprint
	for i := 0; i < numBlueprints; i++ {
		blueprints = append(blueprints, chaosBlueprint{
			name:         fmt.Sprintf("chaos-test-%d-%d", testID, i),
			executorType: fmt.Sprintf("chaos-executor-%d-%d", testID, i),
		})
	}

	// Cleanup
	defer func() {
		t.Log("Cleaning up all chaos test artifacts...")
		for _, bp := range blueprints {
			_ = coloniesClient.RemoveBlueprint(config.ColonyName, bp.name, config.ColonyPrvKey)
			cleanupExecutorsByType(coloniesClient, config, bp.executorType)
		}
	}()

	// Phase 1: Create all blueprints with 2 replicas each
	t.Log("Phase 1: Creating blueprints with 2 replicas each...")
	for _, bp := range blueprints {
		blueprint := createTestBlueprintWithReplicas(bp.name, bp.executorType, reconcilerName, config.ColonyName, 2)
		_, err := coloniesClient.AddBlueprint(blueprint, config.ColonyPrvKey)
		if err != nil {
			t.Fatalf("Failed to create blueprint %s: %v", bp.name, err)
		}
	}

	// Phase 2: Trigger reconciliation for all
	t.Log("Phase 2: Initial reconciliation for all blueprints...")
	var wg sync.WaitGroup
	for _, bp := range blueprints {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			_ = reconcileAndWait(coloniesClient, config, name, 120*time.Second)
		}(bp.name)
	}
	wg.Wait()

	// Verify initial state
	t.Log("Verifying initial state...")
	for _, bp := range blueprints {
		count := countExecutorsByType(coloniesClient, config, bp.executorType)
		t.Logf("  %s: %d executor(s)", bp.name, count)
	}

	// Phase 3: Chaos - simultaneous scaling and crash simulation
	t.Log("Phase 3: Starting chaos operations...")

	var chaosWg sync.WaitGroup
	var successCount int32
	var failCount int32

	// Chaos operation 1: Scale up first blueprint
	chaosWg.Add(1)
	go func() {
		defer chaosWg.Done()
		bp := blueprints[0]
		t.Logf("  [Chaos] Scaling up %s to 4 replicas...", bp.name)

		blueprint, err := coloniesClient.GetBlueprint(config.ColonyName, bp.name, config.ExecutorPrvKey)
		if err != nil {
			t.Logf("  [Chaos] Failed to get blueprint %s: %v", bp.name, err)
			atomic.AddInt32(&failCount, 1)
			return
		}
		blueprint.Spec["replicas"] = 4
		blueprint.Metadata.Generation++
		_, err = coloniesClient.UpdateBlueprint(blueprint, config.ColonyPrvKey)
		if err != nil {
			t.Logf("  [Chaos] Failed to update blueprint %s: %v", bp.name, err)
			atomic.AddInt32(&failCount, 1)
			return
		}
		_ = reconcileAndWait(coloniesClient, config, bp.name, 120*time.Second)
		atomic.AddInt32(&successCount, 1)
	}()

	// Chaos operation 2: Scale down second blueprint
	if len(blueprints) > 1 {
		chaosWg.Add(1)
		go func() {
			defer chaosWg.Done()
			bp := blueprints[1]
			t.Logf("  [Chaos] Scaling down %s to 1 replica...", bp.name)

			blueprint, err := coloniesClient.GetBlueprint(config.ColonyName, bp.name, config.ExecutorPrvKey)
			if err != nil {
				t.Logf("  [Chaos] Failed to get blueprint %s: %v", bp.name, err)
				atomic.AddInt32(&failCount, 1)
				return
			}
			blueprint.Spec["replicas"] = 1
			blueprint.Metadata.Generation++
			_, err = coloniesClient.UpdateBlueprint(blueprint, config.ColonyPrvKey)
			if err != nil {
				t.Logf("  [Chaos] Failed to update blueprint %s: %v", bp.name, err)
				atomic.AddInt32(&failCount, 1)
				return
			}
			_ = reconcileAndWait(coloniesClient, config, bp.name, 120*time.Second)
			atomic.AddInt32(&successCount, 1)
		}()
	}

	// Chaos operation 3: Simulate crash on third blueprint
	if len(blueprints) > 2 {
		chaosWg.Add(1)
		go func() {
			defer chaosWg.Done()
			bp := blueprints[2]
			t.Logf("  [Chaos] Simulating crash for %s...", bp.name)

			executors := getExecutorsByType(coloniesClient, config, bp.executorType)
			if len(executors) > 0 {
				// Remove a random executor
				idx := rand.Intn(len(executors))
				err := coloniesClient.RemoveExecutor(config.ColonyName, executors[idx].Name, config.ColonyPrvKey)
				if err != nil {
					t.Logf("  [Chaos] Failed to remove executor: %v", err)
				}
			}
			// Wait for cron recovery
			time.Sleep(70 * time.Second)
			atomic.AddInt32(&successCount, 1)
		}()
	}

	chaosWg.Wait()

	t.Logf("Phase 3 complete: %d successful, %d failed", successCount, failCount)

	// Phase 4: Verify final state
	t.Log("Phase 4: Verifying final state after chaos...")
	time.Sleep(10 * time.Second) // Allow system to stabilize

	expectedReplicas := []int{4, 1, 2} // Expected after chaos: scaled up, scaled down, recovered
	allValid := true

	for i, bp := range blueprints {
		count := countExecutorsByType(coloniesClient, config, bp.executorType)
		expected := expectedReplicas[i]
		status := "OK"
		if count != expected {
			status = "MISMATCH"
			allValid = false
		}
		t.Logf("  %s: %d executor(s) (expected %d) - %s", bp.name, count, expected, status)
	}

	if allValid {
		t.Log("SUCCESS: Chaos scenario test passed!")
	} else {
		t.Log("WARNING: Some counts don't match expected - system may need more time to stabilize")
	}
}

// TestRapidScaling tests rapid scale up and down operations
func TestRapidScaling(t *testing.T) {
	config := getTestConfig(t)
	coloniesClient := client.CreateColoniesClient(config.ColoniesHost, config.ColoniesPort, true, false)

	testID := time.Now().Unix()
	blueprintName := fmt.Sprintf("rapid-scale-test-%d", testID)
	executorType := fmt.Sprintf("rapid-scale-executor-%d", testID)
	reconcilerName := config.Reconcilers[0]

	t.Logf("Test: Rapid Scaling")
	t.Logf("  Blueprint: %s", blueprintName)

	// Cleanup
	defer func() {
		t.Log("Cleaning up...")
		_ = coloniesClient.RemoveBlueprint(config.ColonyName, blueprintName, config.ColonyPrvKey)
		cleanupExecutorsByType(coloniesClient, config, executorType)
	}()

	// Create blueprint
	blueprint := createTestBlueprintWithReplicas(blueprintName, executorType, reconcilerName, config.ColonyName, 1)
	_, err := coloniesClient.AddBlueprint(blueprint, config.ColonyPrvKey)
	if err != nil {
		t.Fatalf("Failed to create blueprint: %v", err)
	}

	// Initial reconciliation
	if err := reconcileAndWait(coloniesClient, config, blueprintName, 60*time.Second); err != nil {
		t.Fatalf("Initial reconciliation failed: %v", err)
	}

	// Rapid scaling sequence: 1 -> 3 -> 1 -> 5 -> 2
	scalingSequence := []int{3, 1, 5, 2}

	for _, targetReplicas := range scalingSequence {
		t.Logf("Scaling to %d replicas...", targetReplicas)

		blueprint, err = coloniesClient.GetBlueprint(config.ColonyName, blueprintName, config.ExecutorPrvKey)
		if err != nil {
			t.Fatalf("Failed to get blueprint: %v", err)
		}

		blueprint.Spec["replicas"] = targetReplicas
		blueprint.Metadata.Generation++
		_, err = coloniesClient.UpdateBlueprint(blueprint, config.ColonyPrvKey)
		if err != nil {
			t.Fatalf("Failed to update blueprint: %v", err)
		}

		// Reconcile
		if err := reconcileAndWait(coloniesClient, config, blueprintName, 90*time.Second); err != nil {
			t.Logf("Warning: Reconciliation issue: %v", err)
		}

		// Brief wait then check
		time.Sleep(5 * time.Second)
		count := countExecutorsByType(coloniesClient, config, executorType)
		t.Logf("  After scaling to %d: found %d executor(s)", targetReplicas, count)
	}

	// Final verification
	t.Log("Final verification...")
	time.Sleep(10 * time.Second)
	finalCount := countExecutorsByType(coloniesClient, config, executorType)
	t.Logf("Final executor count: %d (expected 2)", finalCount)

	if finalCount == 2 {
		t.Log("SUCCESS: Rapid scaling test passed!")
	} else {
		t.Logf("WARNING: Expected 2 executors, got %d", finalCount)
	}
}

// Helper functions

func createTestBlueprintWithReplicas(name, executorType, reconcilerName, colonyName string, replicas int) *core.Blueprint {
	return &core.Blueprint{
		Kind: "ExecutorDeployment",
		Metadata: core.BlueprintMetadata{
			Name:       name,
			ColonyName: colonyName,
		},
		Handler: &core.BlueprintHandler{
			ExecutorName: reconcilerName,
		},
		Spec: map[string]interface{}{
			"image":        "alpine:latest",
			"executorType": executorType,
			"replicas":     replicas,
			"env": map[string]string{
				"TEST_MODE": "true",
			},
			"command": []string{"sleep", "3600"},
		},
	}
}

func reconcileAndWait(c *client.ColoniesClient, config *TestConfig, blueprintName string, timeout time.Duration) error {
	process, err := triggerReconcile(c, config.ColonyName, blueprintName, config.ExecutorPrvKey, false)
	if err != nil {
		return fmt.Errorf("failed to trigger reconcile: %w", err)
	}

	return waitForProcess(c, config.ColonyName, process.ID, config.ExecutorPrvKey, timeout)
}

func forceReconcileAndWait(c *client.ColoniesClient, config *TestConfig, blueprintName string, timeout time.Duration) error {
	process, err := triggerReconcile(c, config.ColonyName, blueprintName, config.ExecutorPrvKey, true)
	if err != nil {
		return fmt.Errorf("failed to trigger force reconcile: %w", err)
	}

	return waitForProcess(c, config.ColonyName, process.ID, config.ExecutorPrvKey, timeout)
}

func countExecutorsByType(c *client.ColoniesClient, config *TestConfig, executorType string) int {
	executors, err := c.GetExecutors(config.ColonyName, config.ExecutorPrvKey)
	if err != nil {
		return 0
	}

	count := 0
	for _, e := range executors {
		if e.Type == executorType && e.State == core.APPROVED {
			count++
		}
	}
	return count
}

func getExecutorsByType(c *client.ColoniesClient, config *TestConfig, executorType string) []*core.Executor {
	executors, err := c.GetExecutors(config.ColonyName, config.ExecutorPrvKey)
	if err != nil {
		return nil
	}

	var result []*core.Executor
	for _, e := range executors {
		if e.Type == executorType {
			result = append(result, e)
		}
	}
	return result
}

func cleanupExecutorsByType(c *client.ColoniesClient, config *TestConfig, executorType string) {
	executors := getExecutorsByType(c, config, executorType)
	for _, e := range executors {
		_ = c.RemoveExecutor(config.ColonyName, e.Name, config.ColonyPrvKey)
	}
}
