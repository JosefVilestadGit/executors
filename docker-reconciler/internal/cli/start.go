package cli

import (
	"fmt"
	"os"
	"strconv"

	"github.com/colonyos/executors/docker-reconciler/pkg/build"
	"github.com/colonyos/executors/docker-reconciler/pkg/executor"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(versionCmd)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version and build information",
	Long:  `Print version and build information`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Deployment Controller Executor")
		fmt.Println("Version:", build.BuildVersion)
		fmt.Println("Build Time:", build.BuildTime)
	},
}

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the deployment controller executor",
	Long:  `Start the deployment controller executor to reconcile container deployments`,
	Run: func(cmd *cobra.Command, args []string) {
		parseEnv()

		verbose, err := cmd.Flags().GetBool("verbose")
		if err != nil {
			log.WithFields(log.Fields{"Error": err}).Error("Failed to parse verbose flag")
			os.Exit(-1)
		}

		if verbose {
			log.SetLevel(log.DebugLevel)
		}

		log.WithFields(log.Fields{
			"ColoniesServerHost": coloniesServerHost,
			"ColoniesServerPort": coloniesServerPort,
			"ColoniesInsecure":   coloniesInsecure,
			"ColonyName":         colonyName,
			"ExecutorName":       executorName,
			"ExecutorType":       executorType,
		}).Info("Starting Deployment Controller Executor")

		executor, err := executor.CreateExecutor(
			executor.WithVerbose(verbose),
			executor.WithColoniesServerHost(coloniesServerHost),
			executor.WithColoniesServerPort(coloniesServerPort),
			executor.WithColoniesInsecure(coloniesInsecure),
			executor.WithColonyName(colonyName),
			executor.WithColonyPrvKey(colonyPrvKey),
			executor.WithExecutorName(executorName),
			executor.WithExecutorID(executorID),
			executor.WithExecutorPrvKey(executorPrvKey),
			executor.WithExecutorType(executorType),
		)

		if err != nil {
			log.WithFields(log.Fields{"Error": err}).Error("Failed to create executor")
			os.Exit(-1)
		}

		err = executor.ServeForEver()
		if err != nil {
			log.WithFields(log.Fields{"Error": err}).Error("Executor failed")
			os.Exit(-1)
		}
	},
}

func init() {
	startCmd.Flags().BoolP("verbose", "v", false, "Enable verbose logging")
}

var coloniesServerHost string
var coloniesServerPort int
var coloniesInsecure bool
var colonyName string
var colonyPrvKey string
var executorName string
var executorID string
var executorPrvKey string
var executorType string

func parseEnv() {
	coloniesServerHost = os.Getenv("COLONIES_SERVER_HOST")
	if coloniesServerHost == "" {
		log.Error("COLONIES_SERVER_HOST environment variable not set")
		os.Exit(-1)
	}

	coloniesServerPortStr := os.Getenv("COLONIES_SERVER_PORT")
	if coloniesServerPortStr == "" {
		coloniesServerPort = 443
	} else {
		var err error
		coloniesServerPort, err = strconv.Atoi(coloniesServerPortStr)
		if err != nil {
			log.WithFields(log.Fields{"Error": err}).Error("Failed to parse COLONIES_SERVER_PORT")
			os.Exit(-1)
		}
	}

	coloniesInsecureStr := os.Getenv("COLONIES_INSECURE")
	if coloniesInsecureStr == "true" {
		coloniesInsecure = true
	} else {
		coloniesInsecure = false
	}

	colonyName = os.Getenv("COLONIES_COLONY_NAME")
	if colonyName == "" {
		log.Error("COLONIES_COLONY_NAME environment variable not set")
		os.Exit(-1)
	}

	colonyPrvKey = os.Getenv("COLONIES_COLONY_PRVKEY")
	executorName = os.Getenv("COLONIES_EXECUTOR_NAME")
	executorID = os.Getenv("COLONIES_EXECUTOR_ID")
	executorPrvKey = os.Getenv("COLONIES_PRVKEY")

	if colonyPrvKey == "" && executorPrvKey == "" {
		log.Error("Neither COLONIES_COLONY_PRVKEY nor COLONIES_PRVKEY environment variables are set")
		os.Exit(-1)
	}

	if executorName == "" {
		log.Error("COLONIES_EXECUTOR_NAME environment variable not set")
		os.Exit(-1)
	}

	executorType = os.Getenv("COLONIES_EXECUTOR_TYPE")
	if executorType == "" {
		executorType = "docker-executor-reconciler"
	}
}
