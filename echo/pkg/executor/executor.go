package executor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/colonyos/colonies/pkg/client"
	"github.com/colonyos/colonies/pkg/core"
	log "github.com/sirupsen/logrus"
)

type Executor struct {
	coloniesServerHost string
	coloniesServerPort int
	coloniesInsecure   bool
	colonyName         string
	executorName       string
	executorPrvKey     string
	ctx                context.Context
	cancel             context.CancelFunc
	client             *client.ColoniesClient
}

type ExecutorOption func(*Executor)

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

func WithExecutorName(name string) ExecutorOption {
	return func(e *Executor) {
		e.executorName = name
	}
}

func WithExecutorPrvKey(key string) ExecutorOption {
	return func(e *Executor) {
		e.executorPrvKey = key
	}
}

func CreateExecutor(opts ...ExecutorOption) (*Executor, error) {
	e := &Executor{}
	for _, opt := range opts {
		opt(e)
	}

	// Validate required fields
	if e.executorName == "" {
		return nil, fmt.Errorf("executor name is required (set COLONIES_EXECUTOR_NAME)")
	}
	if e.executorPrvKey == "" {
		return nil, fmt.Errorf("executor private key is required (set COLONIES_PRVKEY)")
	}

	ctx, cancel := context.WithCancel(context.Background())
	e.ctx = ctx
	e.cancel = cancel

	sigc := make(chan os.Signal)
	signal.Notify(sigc, syscall.SIGHUP, syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGINT, syscall.SIGSEGV)
	go func() {
		<-sigc
		e.Shutdown()
		os.Exit(1)
	}()

	e.client = client.CreateColoniesClient(e.coloniesServerHost, e.coloniesServerPort, e.coloniesInsecure, false)

	// Register the echo function (executor is already registered by docker-reconciler)
	function := &core.Function{
		ExecutorName: e.executorName,
		ColonyName:   e.colonyName,
		FuncName:     "echo",
	}

	_, err := e.client.AddFunction(function, e.executorPrvKey)
	if err != nil {
		log.WithFields(log.Fields{"Error": err}).Warning("Failed to add function")
	}

	log.WithFields(log.Fields{"ExecutorName": e.executorName}).Info("Started executor")

	return e, nil
}

func (e *Executor) Shutdown() error {
	log.WithFields(log.Fields{"ExecutorName": e.executorName}).Info("Shutting down")
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
					log.Info(err)
					continue
				}
			}

			log.Error(err)
			log.Error("Retrying in 5 seconds ...")
			time.Sleep(5 * time.Second)

			continue
		}

		log.WithFields(log.Fields{"ProcessID": process.ID, "ExecutorName": e.executorName}).Info("Assigned process to executor")

		funcName := process.FunctionSpec.FuncName
		if funcName == "echo" {
			if len(process.FunctionSpec.Args) != 1 {
				log.Info(err)
				err = e.client.Fail(process.ID, []string{"Invalid argument"}, e.executorPrvKey)
			}
			textIf := process.FunctionSpec.Args[0]
			text, ok := textIf.(string)
			if !ok {
				log.Info(err)
				err = e.client.Fail(process.ID, []string{"Invalid argument, not string"}, e.executorPrvKey)
				continue
			}

			log.WithFields(log.Fields{"Text": text}).Info("Executing echo function")

			// Add log entry so it shows up with --follow
			e.client.AddLog(process.ID, text, e.executorPrvKey)

			// Small delay to ensure log is persisted before closing
			time.Sleep(100 * time.Millisecond)

			output := make([]interface{}, 1)
			output[0] = text
			err = e.client.CloseWithOutput(process.ID, output, e.executorPrvKey)
			log.Info("Closing process")
		} else {
			log.WithFields(log.Fields{"ProcessID": process.ID, "ExecutorName": e.executorName, "FuncName": funcName}).Info("Unsupported function")
			err = e.client.Fail(process.ID, []string{"Unsupported function: " + funcName}, e.executorPrvKey)
			log.Info(err)
		}
	}
}
