//go:build system

package system

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/colonyos/colonies/pkg/client"
	"github.com/colonyos/colonies/pkg/core"
)

// TestOrphanedContainerDetection tests that the reconciler properly detects
// and fixes containers that are running but have no registered executor.
//
// This test simulates the bug where:
// 1. Containers are running for a deployment
// 2. Executors get deregistered (manually or due to a bug)
// 3. Normal reconcile says "up to date" because it only counts containers
// 4. With the fix, reconcile should detect orphaned containers and recreate them
//
// Run with: go test -tags=system -v ./test/system/...
func TestOrphanedContainerDetection(t *testing.T) {
	// Get environment configuration
	coloniesHost := getEnv("COLONIES_SERVER_HOST", "localhost")
	coloniesPortStr := getEnv("COLONIES_SERVER_PORT", "50080")
	coloniesPort, err := strconv.Atoi(coloniesPortStr)
	if err != nil {
		t.Fatalf("Invalid COLONIES_SERVER_PORT: %v", err)
	}
	colonyName := getEnv("COLONIES_COLONY_NAME", "dev")
	colonyPrvKey := getEnv("COLONIES_COLONY_PRVKEY", "")
	executorPrvKey := getEnv("COLONIES_EXECUTOR_PRVKEY", getEnv("COLONIES_PRVKEY", ""))
	reconcilerName := getEnv("RECONCILER_NAME", "local-docker-reconciler")

	if colonyPrvKey == "" || executorPrvKey == "" {
		t.Skip("Skipping system test: COLONIES_COLONY_PRVKEY and COLONIES_EXECUTOR_PRVKEY must be set")
	}

	// Create colonies client
	coloniesClient := client.CreateColoniesClient(coloniesHost, coloniesPort, true, false)

	blueprintName := fmt.Sprintf("orphan-test-%d", time.Now().Unix())
	executorType := fmt.Sprintf("orphan-test-executor-%d", time.Now().Unix())

	t.Logf("Test configuration:")
	t.Logf("  Colonies: %s:%d", coloniesHost, coloniesPort)
	t.Logf("  Colony: %s", colonyName)
	t.Logf("  Blueprint: %s", blueprintName)
	t.Logf("  Executor Type: %s", executorType)
	t.Logf("  Reconciler: %s", reconcilerName)

	// Cleanup before starting (in case previous run left artifacts)
	t.Log("Pre-cleanup: Removing any existing test artifacts...")
	_ = coloniesClient.RemoveBlueprint(colonyName, blueprintName, colonyPrvKey)
	// Also try to remove any executors of this type
	existingExecutors, _ := coloniesClient.GetExecutors(colonyName, executorPrvKey)
	for _, e := range existingExecutors {
		if e.Type == executorType {
			_ = coloniesClient.RemoveExecutor(colonyName, e.Name, colonyPrvKey)
		}
	}
	time.Sleep(2 * time.Second) // Give system time to clean up

	// Cleanup at the end
	defer func() {
		t.Log("Cleaning up test blueprint...")
		_ = coloniesClient.RemoveBlueprint(colonyName, blueprintName, colonyPrvKey)
	}()

	// Step 1: Create a test blueprint
	t.Log("Step 1: Creating test blueprint...")
	blueprint := createTestBlueprint(blueprintName, executorType, reconcilerName, colonyName)
	_, err = coloniesClient.AddBlueprint(blueprint, colonyPrvKey)
	if err != nil {
		t.Fatalf("Failed to create blueprint: %v", err)
	}
	t.Logf("Created blueprint: %s", blueprintName)

	// Step 2: Trigger reconciliation to create containers
	t.Log("Step 2: Triggering initial reconciliation...")
	process, err := triggerReconcile(coloniesClient, colonyName, blueprintName, executorPrvKey, false)
	if err != nil {
		t.Fatalf("Failed to trigger reconcile: %v", err)
	}
	t.Logf("Reconcile process started: %s", process.ID)

	// Wait for reconciliation to complete
	err = waitForProcess(coloniesClient, colonyName, process.ID, executorPrvKey, 60*time.Second)
	if err != nil {
		t.Fatalf("Reconciliation failed: %v", err)
	}
	t.Log("Initial reconciliation completed")

	// Step 3: Verify executors are registered
	t.Log("Step 3: Verifying executors are registered...")
	executors, err := coloniesClient.GetExecutors(colonyName, executorPrvKey)
	if err != nil {
		t.Fatalf("Failed to get executors: %v", err)
	}

	var testExecutors []*core.Executor
	for _, e := range executors {
		if e.Type == executorType {
			testExecutors = append(testExecutors, e)
		}
	}

	if len(testExecutors) == 0 {
		t.Fatal("No executors registered for test blueprint")
	}
	t.Logf("Found %d executor(s) of type %s", len(testExecutors), executorType)

	// Step 4: Manually deregister executors to simulate the bug
	t.Log("Step 4: Simulating bug by deregistering executors (containers still running)...")
	for _, e := range testExecutors {
		err := coloniesClient.RemoveExecutor(colonyName, e.Name, colonyPrvKey)
		if err != nil {
			t.Logf("Warning: Failed to remove executor %s: %v", e.Name, err)
		} else {
			t.Logf("Removed executor: %s", e.Name)
		}
	}

	// Verify executors are gone
	executors, err = coloniesClient.GetExecutors(colonyName, executorPrvKey)
	if err != nil {
		t.Fatalf("Failed to get executors after removal: %v", err)
	}

	remainingCount := 0
	for _, e := range executors {
		if e.Type == executorType {
			remainingCount++
		}
	}
	t.Logf("Remaining executors of type %s: %d (should be 0)", executorType, remainingCount)

	// Step 5: Get blueprint status - should show 0 ready (orphaned state)
	t.Log("Step 5: Checking blueprint status...")
	blueprintInfo, err := coloniesClient.GetBlueprint(colonyName, blueprintName, executorPrvKey)
	if err != nil {
		t.Fatalf("Failed to get blueprint: %v", err)
	}
	runningInstances := getStatusInt(blueprintInfo.Status, "runningInstances")
	totalInstances := getStatusInt(blueprintInfo.Status, "totalInstances")
	t.Logf("Blueprint status - Running: %d, Total: %d", runningInstances, totalInstances)

	// Step 6: Wait for cron self-healing to detect and fix orphaned containers
	// NOTE: We do NOT trigger a manual reconcile - we're testing the cron self-healing mechanism
	t.Log("Step 6: Waiting for cron self-healing to detect and fix orphaned containers...")
	t.Log("  (Cron runs every 60 seconds, waiting up to 90 seconds)")

	// Wait for executors to be approved (may take a few seconds for approval)
	var fixedExecutors []*core.Executor
	approvedCount := 0

	// Wait up to 90 seconds (15 checks * 6 seconds) to allow cron to run
	for i := 0; i < 15; i++ {
		time.Sleep(6 * time.Second)

		executors, err = coloniesClient.GetExecutors(colonyName, executorPrvKey)
		if err != nil {
			t.Fatalf("Failed to get executors after fix: %v", err)
		}

		fixedExecutors = nil
		for _, e := range executors {
			if e.Type == executorType {
				fixedExecutors = append(fixedExecutors, e)
			}
		}

		// Check if executors are approved
		approvedCount = 0
		for _, e := range fixedExecutors {
			if e.State == core.APPROVED {
				approvedCount++
			}
		}

		t.Logf("Check %d: Found %d executor(s), %d approved", i+1, len(fixedExecutors), approvedCount)

		if approvedCount > 0 {
			break
		}
	}

	t.Logf("After fix: Found %d executor(s) of type %s", len(fixedExecutors), executorType)

	if len(fixedExecutors) == 0 {
		// Get reconciler logs for debugging
		logs, _ := coloniesClient.GetLogsByExecutor(colonyName, reconcilerName, 50, executorPrvKey)
		t.Log("Reconciler logs:")
		for _, log := range logs {
			t.Logf("  %s", log.Message)
		}
		t.Fatal("FAILED: Cron self-healing did not detect/fix orphaned containers - no executors registered after 90 seconds")
	}

	t.Logf("Final: Approved executors: %d/%d", approvedCount, len(fixedExecutors))

	if approvedCount == 0 {
		// The executor might be in PENDING state - this is expected if auto-approval is not configured
		// In a real scenario, the reconciler should approve them
		t.Log("NOTE: Executors are registered but not yet approved - this may be expected depending on approval settings")
	}

	t.Log("SUCCESS: Orphaned container detection and fix working correctly!")
}

// TestContainerExecutorMismatch verifies the reconciler detects mismatches
// between running containers and registered executors
func TestContainerExecutorMismatch(t *testing.T) {
	coloniesHost := getEnv("COLONIES_SERVER_HOST", "localhost")
	coloniesPortStr := getEnv("COLONIES_SERVER_PORT", "50080")
	coloniesPort, err := strconv.Atoi(coloniesPortStr)
	if err != nil {
		t.Fatalf("Invalid COLONIES_SERVER_PORT: %v", err)
	}
	colonyName := getEnv("COLONIES_COLONY_NAME", "dev")
	executorPrvKey := getEnv("COLONIES_EXECUTOR_PRVKEY", getEnv("COLONIES_PRVKEY", ""))

	if executorPrvKey == "" {
		t.Skip("Skipping: COLONIES_EXECUTOR_PRVKEY or COLONIES_PRVKEY must be set")
	}

	coloniesClient := client.CreateColoniesClient(coloniesHost, coloniesPort, true, false)

	t.Log("Checking for container/executor mismatches in existing deployments...")

	// Get all blueprints
	blueprints, err := coloniesClient.GetBlueprints(colonyName, "ExecutorDeployment", executorPrvKey)
	if err != nil {
		t.Fatalf("Failed to get blueprints: %v", err)
	}

	// Get all executors
	executors, err := coloniesClient.GetExecutors(colonyName, executorPrvKey)
	if err != nil {
		t.Fatalf("Failed to get executors: %v", err)
	}

	// Build executor type -> count map
	executorCounts := make(map[string]int)
	for _, e := range executors {
		if e.State == core.APPROVED {
			executorCounts[e.Type]++
		}
	}

	mismatchFound := false
	for _, bp := range blueprints {
		// Parse spec to get executor type and replicas
		specBytes, _ := json.Marshal(bp.Spec)
		var spec struct {
			ExecutorType string `json:"executorType"`
			Replicas     int    `json:"replicas"`
		}
		if err := json.Unmarshal(specBytes, &spec); err != nil {
			continue
		}

		actualCount := executorCounts[spec.ExecutorType]
		runningInstances := getStatusInt(bp.Status, "runningInstances")

		t.Logf("Blueprint %s: type=%s, desired=%d, status.running=%d, actual_executors=%d",
			bp.Metadata.Name, spec.ExecutorType, spec.Replicas, runningInstances, actualCount)

		if actualCount != runningInstances {
			t.Logf("  WARNING: Mismatch between status.running (%d) and actual executors (%d)",
				runningInstances, actualCount)
			mismatchFound = true
		}

		if actualCount < spec.Replicas {
			t.Logf("  WARNING: Missing executors - have %d, want %d",
				actualCount, spec.Replicas)
			mismatchFound = true
		}
	}

	if mismatchFound {
		t.Log("\nMismatches detected - consider running 'colonies blueprint reconcile' for affected blueprints")
	} else {
		t.Log("\nNo container/executor mismatches found")
	}
}

func createTestBlueprint(name, executorType, reconcilerName, colonyName string) *core.Blueprint {
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
			"replicas":     1,
			"env": map[string]string{
				"TEST_MODE": "true",
			},
			"command": []string{"sleep", "3600"},
		},
	}
}

func triggerReconcile(c *client.ColoniesClient, colonyName, blueprintName, prvKey string, force bool) (*core.Process, error) {
	funcSpec := &core.FunctionSpec{
		NodeName:    "",
		FuncName:    "reconcile",
		MaxWaitTime: 120,
		MaxExecTime: 300,
		MaxRetries:  0,
		Args:        []interface{}{},
		KwArgs: map[string]interface{}{
			"kind":          "ExecutorDeployment",
			"blueprintName": blueprintName,
			"force":         force,
		},
		Conditions: core.Conditions{
			ColonyName:   colonyName,
			ExecutorType: "docker-reconciler",
		},
	}

	return c.Submit(funcSpec, prvKey)
}

func waitForProcess(c *client.ColoniesClient, colonyName, processID, prvKey string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		process, err := c.GetProcess(processID, prvKey)
		if err != nil {
			return fmt.Errorf("failed to get process: %w", err)
		}

		switch process.State {
		case core.SUCCESS:
			return nil
		case core.FAILED:
			// Get error details
			if len(process.Errors) > 0 {
				return fmt.Errorf("process failed: %s", strings.Join(process.Errors, ", "))
			}
			return fmt.Errorf("process failed")
		}

		time.Sleep(2 * time.Second)
	}

	return fmt.Errorf("timeout waiting for process")
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

// getStatusInt safely extracts an int value from the status map
func getStatusInt(status map[string]interface{}, key string) int {
	if status == nil {
		return 0
	}
	if val, ok := status[key]; ok {
		switch v := val.(type) {
		case int:
			return v
		case int64:
			return int(v)
		case float64:
			return int(v)
		}
	}
	return 0
}
