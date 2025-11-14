package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

const (
	TimeLayout    = "2006-01-02 15:04:05"
	KEYCHAIN_PATH = ".colonies"
)

var (
	Verbose               bool
	ColoniesServerHost    string
	ColoniesServerPort    int
	ColoniesInsecure      bool
	ColoniesSkipTLSVerify bool
	ColoniesUseTLS        bool
	ColonyID              string
	ColonyName            string
	ColonyPrvKey          string
	ExecutorName          string
	ExecutorID            string
	ExecutorType          string
	ExecutorPrvKey        string
	Long                  float64
	Lat                   float64
	LocDesc               string
)

func init() {
	rootCmd.PersistentFlags().BoolVarP(&Verbose, "verbose", "v", false, "verbose output")
}

var rootCmd = &cobra.Command{
	Use:   "sleep_executor",
	Short: "Colonies Sleep executor",
	Long:  "Colonies Sleep executor",
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
