package cli

import (
	"errors"
	"os"
	"strconv"

	"github.com/colonyos/executors/sleep/pkg/build"
	"github.com/colonyos/executors/sleep/pkg/executor"
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
			"ColonyID":           ColonyID,
			"ColonyPrvKey":       "***********************",
			"ExecutorId":         ExecutorID,
			"ExecutorName":       ExecutorName,
			"ExecutorPrvKey":     "***********************",
			"Longitude":          Long,
			"Latitude":           Lat,
			"LocationDesc":       LocDesc,
		}).
			Info("Starting a Colonies Sleep Executor")

		executor, err := executor.CreateExecutor(
			executor.WithColoniesServerHost(ColoniesServerHost),
			executor.WithColoniesServerPort(ColoniesServerPort),
			executor.WithColoniesInsecure(ColoniesInsecure),
			executor.WithColonyPrvKey(ColonyPrvKey),
			executor.WithColonyID(ColonyID),
			executor.WithColonyName(ColonyName),
			executor.WithExecutorID(ExecutorID),
			executor.WithExecutorName(ExecutorName),
			executor.WithExecutorPrvKey(ExecutorPrvKey),
			executor.WithLong(Long),
			executor.WithLat(Lat),
			executor.WithLocDesc(LocDesc),
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

	if ColonyID == "" {
		ColonyID = os.Getenv("COLONIES_COLONY_ID")
	}
	if ColonyID == "" {
		CheckError(errors.New("Unknown Colony Id"))
	}

	if ColonyPrvKey == "" {
		ColonyPrvKey = os.Getenv("COLONIES_COLONY_PRVKEY")
	}

	if ColonyName == "" {
		ColonyName = os.Getenv("COLONIES_COLONY_NAME")
	}

	if ExecutorID == "" {
		ExecutorID = os.Getenv("COLONIES_EXECUTOR_ID")
	}
	if ExecutorID == "" {
		CheckError(errors.New("Unknown Executor Id"))
	}

	if ExecutorName == "" {
		ExecutorName = os.Getenv("COLONIES_EXECUTOR_NAME")
	}

	if ExecutorPrvKey == "" {
		ExecutorPrvKey = os.Getenv("COLONIES_EXECUTOR_PRVKEY")
	}
	if ExecutorPrvKey == "" {
		ExecutorPrvKey = os.Getenv("COLONIES_PRVKEY")
	}

	LocDesc = os.Getenv("EXECUTOR_LOCATION_DESC")

	longStr := os.Getenv("EXECUTOR_LOCATION_LONG")
	Long, err = strconv.ParseFloat(longStr, 64)
	if err != nil {
		log.Error("Failed to set location longitude")
	}

	latStr := os.Getenv("EXECUTOR_LOCATION_LAT")
	Lat, err = strconv.ParseFloat(latStr, 64)
	if err != nil {
		log.Error("Failed to set location latitude")
	}
}

func CheckError(err error) {
	if err != nil {
		log.WithFields(log.Fields{"Error": err, "BuildVersion": build.BuildVersion, "BuildTime": build.BuildTime}).Error(err.Error())
		os.Exit(-1)
	}
}
