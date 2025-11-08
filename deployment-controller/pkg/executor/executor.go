package executor

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/colonyos/colonies/pkg/client"
	"github.com/colonyos/colonies/pkg/core"
	"github.com/colonyos/colonies/pkg/security/crypto"
	"github.com/colonyos/executors/deployment-controller/pkg/reconciler"
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

	return executor, executorID, executorPrvKey, nil
}

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

	// Register the reconcile function
	function := &core.Function{ExecutorName: e.executorName, ColonyName: e.colonyName, FuncName: "reconcile"}
	_, err = e.client.AddFunction(function, e.executorPrvKey)
	if err != nil {
		log.WithFields(log.Fields{"Error": err}).Warning("Failed to add reconcile function")
	}

	// Create reconciler
	e.reconciler, err = reconciler.CreateReconciler(e.client, e.executorPrvKey)
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

func (e *Executor) ServeForEver() error {
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
			go e.handleReconcile(process)
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

	// For deployment-controller, we work with the "New" resource (the desired state)
	// The reconciliation.New contains the current/desired resource state
	// The reconciliation.Old contains the previous state (nil for create, set for update)
	var resource *core.Resource
	if reconciliation.New != nil {
		resource = reconciliation.New
	} else if reconciliation.Old != nil {
		// For delete operations, Old is set and New is nil
		resource = reconciliation.Old
	} else {
		e.failProcess(process, "No resource found in reconciliation data")
		return
	}

	log.WithFields(log.Fields{
		"ResourceName": resource.Metadata.Name,
		"ResourceKind": resource.Kind,
		"Action":       reconciliation.Action,
	}).Info("Processing resource reconciliation")

	// Perform reconciliation
	if err := e.reconciler.Reconcile(process, resource); err != nil {
		e.failProcess(process, "Reconciliation failed: "+err.Error())
		return
	}

	// Close the process as successful
	if err := e.client.Close(process.ID, e.executorPrvKey); err != nil {
		log.WithFields(log.Fields{"ProcessID": process.ID, "Error": err}).Error("Failed to close process")
	} else {
		log.WithFields(log.Fields{"ProcessID": process.ID}).Info("Process completed successfully")
	}
}

func (e *Executor) failProcess(process *core.Process, reason string) {
	log.WithFields(log.Fields{"ProcessID": process.ID, "Reason": reason}).Error("Process failed")
	err := e.client.Fail(process.ID, []string{reason}, e.executorPrvKey)
	if err != nil {
		log.WithFields(log.Fields{"ProcessID": process.ID, "Error": err}).Error("Failed to mark process as failed")
	}
}
