package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	GIT_COMMIT_SHA = ""
	BUILD_TIME     = ""
)

// versionCmd represents the version command
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Display version details",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("GIT_COMMIT_SHA:", GIT_COMMIT_SHA)
		fmt.Println("BUILD_TIME:", BUILD_TIME)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
