package executor

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/colonyos/colonies/pkg/client"
	"github.com/colonyos/colonies/pkg/core"
	"github.com/colonyos/colonies/pkg/security/crypto"
	"github.com/colonyos/executors/docker-reconciler/pkg/reconciler"
	log "github.com/sirupsen/logrus"
)

type Executor struct {
	verbose            bool
	coloniesServerHost string
	coloniesServerPort int
	coloniesInsecure   bool
	colonyName         string
	colonyPrvKey       string
	executorName       string
	executorID         string
	executorPrvKey     string
	executorType       string
	ctx                context.Context
	cancel             context.CancelFunc
	client             *client.ColoniesClient
	reconciler         *reconciler.Reconciler
	managedResources   map[string]*core.Blueprint // resourceID -> blueprint
	resourcesMutex     sync.RWMutex
}

type ExecutorOption func(*Executor)

func WithVerbose(verbose bool) ExecutorOption {
	return func(e *Executor) {
		e.verbose = verbose
	}
}

func WithColoniesServerHost(host string) ExecutorOption {
	return func(e *Executor) {
		e.coloniesServerHost = host
	}
}

func WithColoniesServerPort(port int) ExecutorOption {
	return func(e *Executor) {
		e.coloniesServerPort = port
	}
}

func WithExecutorType(executorType string) ExecutorOption {
	return func(e *Executor) {
		e.executorType = executorType
	}
}

func WithColoniesInsecure(insecure bool) ExecutorOption {
	return func(e *Executor) {
		e.coloniesInsecure = insecure
	}
}

func WithColonyName(name string) ExecutorOption {
	return func(e *Executor) {
		e.colonyName = name
	}
}

func WithColonyPrvKey(prvkey string) ExecutorOption {
	return func(e *Executor) {
		e.colonyPrvKey = prvkey
	}
}

func WithExecutorName(executorName string) ExecutorOption {
	return func(e *Executor) {
		e.executorName = executorName
	}
}

func WithExecutorID(executorID string) ExecutorOption {
	return func(e *Executor) {
		e.executorID = executorID
	}
}

func WithExecutorPrvKey(key string) ExecutorOption {
	return func(e *Executor) {
		e.executorPrvKey = key
	}
}

func (e *Executor) createColoniesExecutorWithKey(colonyName string) (*core.Executor, string, string, error) {
	crypto := crypto.CreateCrypto()
	executorPrvKey, err := crypto.GeneratePrivateKey()
	if err != nil {
		return nil, "", "", err
	}

	executorID, err := crypto.GenerateID(executorPrvKey)
	if err != nil {
		return nil, "", "", err
	}

	executor := core.CreateExecutor(executorID, e.executorType, e.executorName, colonyName, time.Now(), time.Now())

	// Add node metadata for auto-registration
	nodeMetadata := detectNodeMetadata()
	executor.NodeMetadata = nodeMetadata

	return executor, executorID, executorPrvKey, nil
}

// detectMemoryMB detects total system memory in MB
func detectMemoryMB() int64 {
	if runtime.GOOS != "linux" {
		return 0
	}

	file, err := os.Open("/proc/meminfo")
	if err != nil {
		log.WithFields(log.Fields{"Error": err}).Warn("Failed to open /proc/meminfo")
		return 0
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				memKB, err := strconv.ParseInt(fields[1], 10, 64)
				if err == nil {
					return memKB / 1024 // Convert KB to MB
				}
			}
		}
	}
	return 0
}

// GPUInfo contains information about a GPU
type GPUInfo struct {
	Index  int
	Name   string
	Memory int64 // Memory in MB
}

// detectGPUs detects GPUs and their properties (NVIDIA only for now)
func detectGPUs() []GPUInfo {
	gpus := []GPUInfo{}

	if runtime.GOOS != "linux" {
		return gpus
	}

	// Try nvidia-smi first (most reliable)
	gpus = detectGPUsViaNvidiaSmi()
	if len(gpus) > 0 {
		return gpus
	}

	// Fallback: check for NVIDIA GPUs by counting devices in /proc/driver/nvidia/gpus/
	gpuDir := "/proc/driver/nvidia/gpus"
	entries, err := os.ReadDir(gpuDir)
	if err == nil {
		index := 0
		for _, entry := range entries {
			if entry.IsDir() {
				// Try to read GPU information
				infoPath := filepath.Join(gpuDir, entry.Name(), "information")
				name := "Unknown GPU"

				if data, err := os.ReadFile(infoPath); err == nil {
					// Parse the information file for Model name
					lines := strings.Split(string(data), "\n")
					for _, line := range lines {
						if strings.HasPrefix(line, "Model:") {
							parts := strings.SplitN(line, ":", 2)
							if len(parts) == 2 {
								name = strings.TrimSpace(parts[1])
							}
						}
					}
				}

				gpus = append(gpus, GPUInfo{
					Index:  index,
					Name:   name,
					Memory: 0, // Memory not available from /proc
				})
				index++
			}
		}
		return gpus
	}

	// Alternative: check for /dev/nvidia* devices
	devices, err := filepath.Glob("/dev/nvidia[0-9]*")
	if err == nil {
		for i := range devices {
			gpus = append(gpus, GPUInfo{
				Index:  i,
				Name:   "Unknown GPU",
				Memory: 0,
			})
		}
	}

	return gpus
}

// detectGPUsViaNvidiaSmi uses nvidia-smi to detect GPUs
func detectGPUsViaNvidiaSmi() []GPUInfo {
	gpus := []GPUInfo{}

	// Check if nvidia-smi exists
	nvidiaSmiPath, err := exec.LookPath("nvidia-smi")
	if err != nil {
		// nvidia-smi not found
		return gpus
	}

	// Run nvidia-smi to get GPU info in CSV format
	// Format: index, name, memory.total
	cmd := exec.Command(nvidiaSmiPath, "--query-gpu=index,name,memory.total", "--format=csv,noheader,nounits")
	var out bytes.Buffer
	cmd.Stdout = &out
	err = cmd.Run()
	if err != nil {
		log.WithFields(log.Fields{"Error": err}).Debug("Failed to run nvidia-smi")
		return gpus
	}

	// Parse the output
	scanner := bufio.NewScanner(&out)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, ", ")
		if len(parts) >= 3 {
			index, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
			name := strings.TrimSpace(parts[1])
			memory, err2 := strconv.ParseInt(strings.TrimSpace(parts[2]), 10, 64)

			if err1 == nil && err2 == nil {
				gpus = append(gpus, GPUInfo{
					Index:  index,
					Name:   name,
					Memory: memory, // Memory is in MB from nvidia-smi
				})
			}
		}
	}

	return gpus
}

func detectNodeMetadata() *core.NodeMetadata {
	// Use COLONIES_NODE_NAME if set, otherwise fall back to hostname
	nodeName := os.Getenv("COLONIES_NODE_NAME")
	if nodeName == "" {
		hostname, err := os.Hostname()
		if err != nil {
			nodeName = "unknown"
		} else {
			nodeName = hostname
		}
	}

	location := os.Getenv("COLONIES_NODE_LOCATION")
	if location == "" {
		location = "default"
	}

	// Detect GPUs and populate labels
	gpus := detectGPUs()
	labels := make(map[string]string)

	for _, gpu := range gpus {
		indexStr := strconv.Itoa(gpu.Index)
		labels["gpu."+indexStr+".name"] = gpu.Name
		if gpu.Memory > 0 {
			labels["gpu."+indexStr+".memory"] = strconv.FormatInt(gpu.Memory, 10)
		}
	}

	metadata := &core.NodeMetadata{
		Hostname:     nodeName,
		Location:     location,
		Platform:     runtime.GOOS,
		Architecture: runtime.GOARCH,
		CPU:          runtime.NumCPU(),
		Memory:       detectMemoryMB(),
		GPU:          len(gpus),
		Capabilities: []string{"docker"},
		Labels:       labels,
	}

	return metadata
}

func CreateExecutor(opts ...ExecutorOption) (*Executor, error) {
	e := &Executor{
		managedResources: make(map[string]*core.Blueprint),
	}
	for _, opt := range opts {
		opt(e)
	}

	ctx, cancel := context.WithCancel(context.Background())
	e.ctx = ctx
	e.cancel = cancel

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGHUP, syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGINT, syscall.SIGSEGV)
	go func() {
		<-sigc
		e.Shutdown()
		os.Exit(1)
	}()

	e.client = client.CreateColoniesClient(e.coloniesServerHost, e.coloniesServerPort, e.coloniesInsecure, false)

	var err error
	if e.colonyPrvKey != "" {
		spec, executorID, executorPrvKey, err := e.createColoniesExecutorWithKey(e.colonyName)
		if err != nil {
			return nil, err
		}
		e.executorID = executorID
		e.executorPrvKey = executorPrvKey

		_, err = e.client.AddExecutor(spec, e.colonyPrvKey)
		if err != nil {
			return nil, err
		}
		err = e.client.ApproveExecutor(e.colonyName, e.executorName, e.colonyPrvKey)
		if err != nil {
			return nil, err
		}

		log.WithFields(log.Fields{"ColonyName": e.colonyName, "ExecutorName": e.executorName}).Info("Self-registered")
	}

	// Register the reconcile function
	function := &core.Function{ExecutorName: e.executorName, ColonyName: e.colonyName, FuncName: "reconcile"}
	_, err = e.client.AddFunction(function, e.executorPrvKey)
	if err != nil {
		log.WithFields(log.Fields{"Error": err}).Warning("Failed to add reconcile function")
	}

	// Get location from environment for child executors
	location := os.Getenv("COLONIES_NODE_LOCATION")
	if location == "" {
		location = "default"
	}

	// Create reconciler with colony owner key for executor registration
	e.reconciler, err = reconciler.CreateReconciler(e.client, e.executorPrvKey, e.colonyPrvKey, e.colonyName, location)
	if err != nil {
		return nil, err
	}

	log.WithFields(log.Fields{
		"Verbose":            e.verbose,
		"ColoniesServerHost": e.coloniesServerHost,
		"ColoniesServerPort": e.coloniesServerPort,
		"ColoniesInsecure":   e.coloniesInsecure,
		"ColonyName":         e.colonyName,
		"ColonyPrvKey":       "***********************",
		"ExecutorId":         e.executorID,
		"ExecutorName":       e.executorName,
		"ExecutorPrvKey":     "***********************",
		"ExecutorType":       e.executorType}).
		Info("Deployment Controller Executor started")

	return e, nil
}

func (e *Executor) Shutdown() error {
	log.Info("Shutting down")
	if e.colonyPrvKey != "" {
		err := e.client.RemoveExecutor(e.colonyName, e.executorName, e.colonyPrvKey)
		if err != nil {
			log.WithFields(log.Fields{
				"ExecutorID":   e.executorID,
				"ExecutorName": e.executorName,
				"ColonyName":   e.colonyName}).
				Warning("Failed to deregistered")
		}

		log.WithFields(log.Fields{
			"ExecutorID":   e.executorID,
			"ExecutorName": e.executorName,
			"ColonyName":   e.colonyName}).
			Info("Deregistered")
	}
	e.cancel()
	return nil
}

// reconcileStartupState checks all blueprints on startup and reconciles if needed
func (e *Executor) reconcileStartupState() {
	log.Info("Performing startup state reconciliation...")

	// Fetch all ExecutorDeployment blueprints
	blueprints, err := e.client.GetBlueprints(e.colonyName, "ExecutorDeployment", e.colonyPrvKey)
	if err != nil {
		log.WithFields(log.Fields{"Error": err}).Warn("Failed to fetch blueprints on startup")
		return
	}

	if len(blueprints) == 0 {
		log.Info("No blueprints found on startup")
		return
	}

	log.WithFields(log.Fields{"Count": len(blueprints)}).Info("Checking blueprints on startup")

	for _, blueprint := range blueprints {
		// Check if this blueprint is managed by a docker-reconciler
		if blueprint.Spec == nil {
			continue
		}

		executorType, ok := blueprint.Spec["executorType"].(string)
		if !ok || executorType != "docker-reconciler" {
			continue
		}

		// Get current state from reconciler
		status, err := e.reconciler.CollectStatus(blueprint)
		if err != nil {
			log.WithFields(log.Fields{
				"Error":         err,
				"BlueprintName": blueprint.Metadata.Name,
			}).Warn("Failed to collect status on startup")
			continue
		}

		runningInstances, ok := status["runningInstances"].(int)
		if !ok {
			continue
		}

		// Get desired replicas
		var desiredReplicas int
		replicas, ok := blueprint.Spec["replicas"]
		if !ok {
			continue
		}
		switch v := replicas.(type) {
		case int:
			desiredReplicas = v
		case float64:
			desiredReplicas = int(v)
		default:
			continue
		}

		// Check if reconciliation is needed
		needsReconciliation := false
		reason := ""

		if runningInstances != desiredReplicas {
			needsReconciliation = true
			reason = fmt.Sprintf("replica mismatch (running: %d, desired: %d)", runningInstances, desiredReplicas)
		} else {
			// Check generation labels
			hasOldGeneration, err := e.reconciler.HasOldGenerationContainers(blueprint)
			if err != nil {
				log.WithFields(log.Fields{
					"Error":         err,
					"BlueprintName": blueprint.Metadata.Name,
				}).Warn("Failed to check generation labels on startup")
			} else if hasOldGeneration {
				needsReconciliation = true
				reason = "containers with old generation labels detected"
			}
		}

		if needsReconciliation {
			log.WithFields(log.Fields{
				"BlueprintName": blueprint.Metadata.Name,
				"Reason":        reason,
				"Generation":    blueprint.Metadata.Generation,
			}).Info("Startup reconciliation needed")

			// Create a minimal process for reconciliation
			reconciliation := &core.Reconciliation{
				Action: "update",
				New:    blueprint,
				Old:    blueprint,
			}

			process := &core.Process{
				ID: "startup-reconcile-" + blueprint.Metadata.Name,
				FunctionSpec: core.FunctionSpec{
					Reconciliation: reconciliation,
				},
			}

			// Trigger reconciliation
			if err := e.reconciler.Reconcile(process, blueprint); err != nil {
				log.WithFields(log.Fields{
					"Error":         err,
					"BlueprintName": blueprint.Metadata.Name,
				}).Error("Startup reconciliation failed")
			} else {
				log.WithFields(log.Fields{
					"BlueprintName": blueprint.Metadata.Name,
				}).Info("Startup reconciliation completed successfully")
			}

			// Track this blueprint
			e.resourcesMutex.Lock()
			e.managedResources[blueprint.ID] = blueprint
			e.resourcesMutex.Unlock()
		} else {
			log.WithFields(log.Fields{
				"BlueprintName": blueprint.Metadata.Name,
				"Replicas":      desiredReplicas,
			}).Info("Blueprint already at desired state")
		}
	}

	log.Info("Startup state reconciliation completed")
}

func (e *Executor) ServeForEver() error {
	// Perform startup reconciliation before entering main loop
	e.reconcileStartupState()

	for {
		process, err := e.client.AssignWithContext(e.colonyName, 100, e.ctx, "", "", e.executorPrvKey)
		if err != nil {
			var coloniesError *core.ColoniesError
			if errors.As(err, &coloniesError) {
				if coloniesError.Status == 404 { // No processes can be selected for executor
					continue
				}
			}

			log.WithFields(log.Fields{"Error": err}).Error("Failed to assign process to executor")
			log.Error("Retrying in 5 seconds ...")
			time.Sleep(5 * time.Second)
			continue
		}

		log.WithFields(log.Fields{
			"ProcessID":    process.ID,
			"ExecutorID":   e.executorID,
			"ExecutorName": e.executorName,
			"FuncName":     process.FunctionSpec.FuncName}).
			Info("Assigned process to executor")

		// Handle reconcile function
		if process.FunctionSpec.FuncName == "reconcile" {
			e.handleReconcile(process)
		} else {
			log.WithFields(log.Fields{"FuncName": process.FunctionSpec.FuncName}).Error("Unsupported funcname")
			err := e.client.Fail(process.ID, []string{"Unsupported funcname"}, e.executorPrvKey)
			if err != nil {
				log.WithFields(log.Fields{"ProcessId": process.ID, "Error": err}).Error("Failed to close process as failed")
			}
		}
	}
}

func (e *Executor) handleReconcile(process *core.Process) {
	log.WithFields(log.Fields{"ProcessID": process.ID}).Info("Handling reconciliation")

	// Get the reconciliation data from the process FunctionSpec
	if process.FunctionSpec.Reconciliation == nil {
		e.failProcess(process, "No reconciliation data found in process FunctionSpec")
		return
	}

	reconciliation := process.FunctionSpec.Reconciliation

	// For deployment-controller, we work with the "New" blueprint (the desired state)
	// The reconciliation.New contains the current/desired blueprint state
	// The reconciliation.Old contains the previous state (nil for create, set for update)
	var blueprint *core.Blueprint
	if reconciliation.New != nil {
		blueprint = reconciliation.New
	} else if reconciliation.Old != nil {
		// For delete operations, Old is set and New is nil
		blueprint = reconciliation.Old
	} else {
		e.failProcess(process, "No blueprint found in reconciliation data")
		return
	}

	log.WithFields(log.Fields{
		"ResourceName": blueprint.Metadata.Name,
		"ResourceKind": blueprint.Kind,
		"Action":       reconciliation.Action,
	}).Info("Processing blueprint reconciliation")

	// Perform reconciliation
	if err := e.reconciler.Reconcile(process, blueprint); err != nil {
		e.failProcess(process, "Reconciliation failed: "+err.Error())
		return
	}

	// Track or untrack the blueprint based on the action
	if reconciliation.Action == "delete" {
		// Remove from managed blueprints
		e.resourcesMutex.Lock()
		delete(e.managedResources, blueprint.ID)
		e.resourcesMutex.Unlock()
		log.WithFields(log.Fields{"ResourceID": blueprint.ID, "ResourceName": blueprint.Metadata.Name}).Info("Removed blueprint from managed blueprints")
	} else {
		// Add/update in managed blueprints (for create and update actions)
		e.resourcesMutex.Lock()
		e.managedResources[blueprint.ID] = blueprint
		e.resourcesMutex.Unlock()
		log.WithFields(log.Fields{"ResourceID": blueprint.ID, "ResourceName": blueprint.Metadata.Name}).Info("Added/updated blueprint in managed blueprints")
	}

	// Collect status after successful reconciliation
	status, err := e.reconciler.CollectStatus(blueprint)
	if err != nil {
		log.WithFields(log.Fields{"Error": err, "ResourceName": blueprint.Metadata.Name}).Warn("Failed to collect blueprint status")
		// Don't fail the process - reconciliation succeeded, status collection is best-effort
		// Close without status output
		if err := e.client.Close(process.ID, e.executorPrvKey); err != nil {
			log.WithFields(log.Fields{"ProcessID": process.ID, "Error": err}).Error("Failed to close process")
		} else {
			log.WithFields(log.Fields{"ProcessID": process.ID}).Info("Process completed successfully")
		}
	} else {
		// Close the process with status output so the server can update the blueprint
		output := []interface{}{
			map[string]interface{}{
				"status": status,
			},
		}
		if err := e.client.CloseWithOutput(process.ID, output, e.executorPrvKey); err != nil {
			log.WithFields(log.Fields{"ProcessID": process.ID, "Error": err}).Error("Failed to close process with output")
		} else {
			log.WithFields(log.Fields{
				"ProcessID":      process.ID,
				"ResourceName":   blueprint.Metadata.Name,
				"TotalInstances": status["totalInstances"],
			}).Info("Process completed successfully with status update")
		}
	}
}

func (e *Executor) failProcess(process *core.Process, reason string) {
	log.WithFields(log.Fields{"ProcessID": process.ID, "Reason": reason}).Error("Process failed")
	err := e.client.Fail(process.ID, []string{reason}, e.executorPrvKey)
	if err != nil {
		log.WithFields(log.Fields{"ProcessID": process.ID, "Error": err}).Error("Failed to mark process as failed")
	}
}

// selfHealingLoop periodically checks all managed blueprints and triggers reconciliation if state drift is detected
