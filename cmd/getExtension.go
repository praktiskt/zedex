package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var getExtensionCmd = &cobra.Command{
	Use: "extension",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("getExtension called")
	},
}

func init() {
	getCmd.AddCommand(getExtensionCmd)
}
