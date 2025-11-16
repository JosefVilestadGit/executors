package executor

import (
	"context"
	"os"
	"os/signal"
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

	// Add node metadata for auto-registration
	nodeMetadata := detectNodeMetadata()
	executor.NodeMetadata = nodeMetadata

	return executor, executorID, executorPrvKey, nil
}

// CreateExecutor creates and initializes a new Executor
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
