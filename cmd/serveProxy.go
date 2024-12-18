package cmd

import (
	"zedex/zed"

	"github.com/spf13/cobra"
)

var serveProxyCmdConfig = struct {
	localMode bool
}{}

var serveProxyCmd = &cobra.Command{
	Use:   "proxy",
	Short: "Serve a proxy of the Zed extension API.",
	Run: func(cmd *cobra.Command, args []string) {
		api := zed.NewAPI()
		api.Router(serveProxyCmdConfig.localMode).Run(":8080")
	},
}

func init() {
	serveCmd.AddCommand(serveProxyCmd)
	serveProxyCmd.Flags().BoolVar(&serveProxyCmdConfig.localMode, "local-mode", false, "whether to serve extension list and extensions from a local directly or not")
}
