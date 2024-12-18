package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var rootCmdConfig = struct {
	debug bool
}{}

var rootCmd = &cobra.Command{
	Use:   "zedex",
	Short: "Work with Zed extensions.",
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.Flags().BoolVar(&rootCmdConfig.debug, "debug", false, "activate debug logging")
}
