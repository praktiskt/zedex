package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:    "zedex",
	PreRun: func(cmd *cobra.Command, args []string) { manageDefaultFlags() },
	Short:  "A self hosted Zed server.",
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&baseFlags.debug, "debug", false, "activate debug logging")
}
