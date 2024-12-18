package cmd

import (
	"zedex/zed"

	"github.com/spf13/cobra"
)

// serveProxyCmd represents the serveProxy command
var serveProxyCmd = &cobra.Command{
	Use:   "proxy",
	Short: "Serve a proxy of the Zed API",
	Run: func(cmd *cobra.Command, args []string) {
		api := zed.NewAPI()
		api.Router().Run(":8080")
	},
}

func init() {
	serveCmd.AddCommand(serveProxyCmd)
}
