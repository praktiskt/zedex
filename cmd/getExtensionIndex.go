package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var getExtensionIndexCmd = &cobra.Command{
	Use: "extension-index",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("getExtensionIndex called")
	},
}

func init() {
	getCmd.AddCommand(getExtensionIndexCmd)
}
