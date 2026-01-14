package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var Verbose bool
var ColoniesServerHost string
var ColoniesServerPort int
var ColoniesInsecure bool
var ColoniesUseTLS bool
var ColonyName string
var ExecutorName string
var ExecutorPrvKey string

func init() {
	rootCmd.PersistentFlags().BoolVarP(&Verbose, "verbose", "v", false, "verbose output")
}

var rootCmd = &cobra.Command{
	Use:   "echo_executor",
	Short: "Colonies Echo executor",
	Long:  "Colonies Echo executor",
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
