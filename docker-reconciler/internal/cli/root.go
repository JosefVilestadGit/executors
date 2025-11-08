package cli

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "docker-executor-reconciler",
	Short: "ColonyOS Docker Executor Reconciler",
	Long:  `A ColonyOS executor that reconciles Docker container deployments based on ExecutorDeployment services`,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		panic(err)
	}
}
