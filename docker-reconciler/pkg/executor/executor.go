package executor

import (
	"context"
	"fmt"
	"os"
	"os/signal"
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

// Executor is the main deployment controller executor
// Executor is now completely stateless - no managed resources tracking
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
	logMu              sync.Mutex // Mutex for synchronizing log writes
}

// populateCapabilitiesFromEnv populates executor capabilities from environment variables
func populateCapabilitiesFromEnv(executor *core.Executor) {
	// Initialize hardware array with one entry
	executor.Capabilities.Hardware = []core.Hardware{{}}
	hw := &executor.Capabilities.Hardware[0]

	// Populate from env vars
	hw.Model = os.Getenv("EXECUTOR_HW_MODEL")
	hw.CPU = os.Getenv("EXECUTOR_HW_CPU")
	if coresStr := os.Getenv("EXECUTOR_HW_CPU_CORES"); coresStr != "" {
		if cores, err := strconv.Atoi(coresStr); err == nil {
			hw.Cores = cores
		}
	}
	hw.Memory = os.Getenv("EXECUTOR_HW_MEM")
	hw.Storage = os.Getenv("EXECUTOR_HW_STORAGE")
	hw.Platform = os.Getenv("EXECUTOR_HW_PLATFORM")
	hw.Architecture = os.Getenv("EXECUTOR_HW_ARCHITECTURE")
	if network := os.Getenv("EXECUTOR_HW_NETWORK"); network != "" {
		hw.Network = strings.Split(network, ",")
	}
	if nodesStr := os.Getenv("EXECUTOR_HW_NODES"); nodesStr != "" {
		if nodes, err := strconv.Atoi(nodesStr); err == nil {
			hw.Nodes = nodes
		}
	}

	// GPU settings
	hw.GPU.Name = os.Getenv("EXECUTOR_HW_GPU_NAME")
	hw.GPU.Memory = os.Getenv("EXECUTOR_HW_GPU_MEM")
	if gpuCountStr := os.Getenv("EXECUTOR_HW_GPU_COUNT"); gpuCountStr != "" {
		if gpuCount, err := strconv.Atoi(gpuCountStr); err == nil {
			hw.GPU.Count = gpuCount
		}
	}
	if gpuNodeCountStr := os.Getenv("EXECUTOR_HW_GPU_NODES_COUNT"); gpuNodeCountStr != "" {
		if gpuNodeCount, err := strconv.Atoi(gpuNodeCountStr); err == nil {
			hw.GPU.NodeCount = gpuNodeCount
		}
	}

	// Location settings
	executor.Location.Name = os.Getenv("EXECUTOR_LOCATION_NAME")
	executor.Location.Description = os.Getenv("EXECUTOR_LOCATION_DESC")
	if longStr := os.Getenv("EXECUTOR_LOCATION_LONG"); longStr != "" {
		if long, err := strconv.ParseFloat(longStr, 64); err == nil {
			executor.Location.Long = long
		}
	}
	if latStr := os.Getenv("EXECUTOR_LOCATION_LAT"); latStr != "" {
		if lat, err := strconv.ParseFloat(latStr, 64); err == nil {
			executor.Location.Lat = lat
		}
	}
}

// createColoniesExecutorWithKey creates a new executor with generated keys
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

	// Populate capabilities from environment variables
	populateCapabilitiesFromEnv(executor)

	return executor, executorID, executorPrvKey, nil
}

// CreateExecutor creates and initializes a new Executor
func CreateExecutor(opts ...ExecutorOption) (*Executor, error) {
	e := &Executor{}
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

	// Register the reconcile function (fetch-based, used by cron-based reconciliation)
	function := &core.Function{ExecutorName: e.executorName, ColonyName: e.colonyName, FuncName: "reconcile"}
	_, err = e.client.AddFunction(function, e.executorPrvKey)
	if err != nil {
		log.WithFields(log.Fields{"Error": err}).Warning("Failed to add reconcile function")
	}

	// Get location from environment for child executors
	location := os.Getenv("COLONIES_EXECUTOR_LOCATION")
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

// Shutdown gracefully shuts down the executor
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

// Note: ServeForEver is now in reconciliation_loop.go

// addProcessLog adds a log message to the process for visibility via `colonies log get`
// The message is formatted with a timestamp to match logrus style
// This method is thread-safe and can be called from multiple goroutines
func (e *Executor) addProcessLog(process *core.Process, message string) {
	log.Info(message)
	if e.client != nil && process != nil {
		// Lock to ensure log messages are written atomically and in order
		e.logMu.Lock()
		defer e.logMu.Unlock()

		// Format with timestamp to match logrus style: time="2006-01-02T15:04:05Z07:00" level=info msg="message"
		timestamp := time.Now().Format(time.RFC3339)
		formattedMsg := fmt.Sprintf("time=\"%s\" level=info msg=\"%s\"\n", timestamp, message)
		err := e.client.AddLog(process.ID, formattedMsg, e.executorPrvKey)
		if err != nil {
			log.WithFields(log.Fields{"Error": err}).Debug("Failed to add log to process")
		}
	}
}
