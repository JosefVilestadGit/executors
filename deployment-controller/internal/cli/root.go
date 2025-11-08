package cli

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "deployment-controller",
	Short: "ColonyOS Deployment Controller Executor",
	Long:  `A ColonyOS executor that reconciles container deployments based on ExecutorDeployment resources`,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		panic(err)
	}
}
