package cli

import (
	"errors"
	"os"
	"strconv"

	"github.com/colonyos/executors/echo/pkg/build"
	"github.com/colonyos/executors/echo/pkg/executor"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(startCmd)
}

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start executor",
	Long:  "Start executor",
	Run: func(cmd *cobra.Command, args []string) {
		parseEnv()

		if Verbose {
			log.SetLevel(log.DebugLevel)
		}

		log.WithFields(log.Fields{
			"Verbose":            Verbose,
			"ColoniesServerHost": ColoniesServerHost,
			"ColoniesServerPort": ColoniesServerPort,
			"ColoniesInsecure":   ColoniesInsecure,
			"ColonyName":         ColonyName,
			"ExecutorName":       ExecutorName,
			"ExecutorPrvKey":     "***********************"}).
			Info("Starting a Colonies Echo executor")

		executor, err := executor.CreateExecutor(
			executor.WithColoniesServerHost(ColoniesServerHost),
			executor.WithColoniesServerPort(ColoniesServerPort),
			executor.WithColoniesInsecure(ColoniesInsecure),
			executor.WithColonyName(ColonyName),
			executor.WithExecutorName(ExecutorName),
			executor.WithExecutorPrvKey(ExecutorPrvKey),
		)
		CheckError(err)

		err = executor.ServeForEver()
		CheckError(err)
	},
}

func parseEnv() {
	var err error
	ColoniesServerHostEnv := os.Getenv("COLONIES_SERVER_HOST")
	if ColoniesServerHostEnv != "" {
		ColoniesServerHost = ColoniesServerHostEnv
	}

	ColoniesServerPortEnvStr := os.Getenv("COLONIES_SERVER_PORT")
	if ColoniesServerPortEnvStr != "" {
		ColoniesServerPort, err = strconv.Atoi(ColoniesServerPortEnvStr)
		CheckError(err)
	}

	ColoniesTLSEnv := os.Getenv("COLONIES_TLS")
	if ColoniesTLSEnv == "true" {
		ColoniesUseTLS = true
		ColoniesInsecure = false
	} else if ColoniesTLSEnv == "false" {
		ColoniesUseTLS = false
		ColoniesInsecure = true
	}

	VerboseEnv := os.Getenv("COLONIES_VERBOSE")
	if VerboseEnv == "true" {
		Verbose = true
	} else if VerboseEnv == "false" {
		Verbose = false
	}

	if ColonyName == "" {
		ColonyName = os.Getenv("COLONIES_COLONY_NAME")
	}
	if ColonyName == "" {
		CheckError(errors.New("COLONIES_COLONY_NAME is required"))
	}

	if ExecutorName == "" {
		ExecutorName = os.Getenv("COLONIES_EXECUTOR_NAME")
	}
	if ExecutorName == "" {
		CheckError(errors.New("COLONIES_EXECUTOR_NAME is required (injected by docker-reconciler)"))
	}

	if ExecutorPrvKey == "" {
		ExecutorPrvKey = os.Getenv("COLONIES_PRVKEY")
	}
	if ExecutorPrvKey == "" {
		CheckError(errors.New("COLONIES_PRVKEY is required (injected by docker-reconciler)"))
	}
}

func CheckError(err error) {
	if err != nil {
		log.WithFields(log.Fields{"Error": err, "BuildVersion": build.BuildVersion, "BuildTime": build.BuildTime}).Error(err.Error())
		os.Exit(-1)
	}
}
