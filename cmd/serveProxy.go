package cmd

import (
	"fmt"

	"zedex/zed"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var serveProxyCmdConfig = struct {
	localMode bool
	outputDir string
	port      int
}{}

var serveProxyCmd = &cobra.Command{
	Use:   "proxy",
	Short: "Serve a proxy of the Zed extension API.",
	Run: func(cmd *cobra.Command, args []string) {
		zc := zed.NewZedClient(1)
		zc.WithExtensionsLocalDir(serveProxyCmdConfig.outputDir)
		api := zed.NewAPI(serveProxyCmdConfig.localMode, zc)

		log.Infof("serving on %v", serveProxyCmdConfig.port)
		api.Router().Run(fmt.Sprintf(":%v", serveProxyCmdConfig.port))
	},
}

func init() {
	serveCmd.AddCommand(serveProxyCmd)
	serveProxyCmd.Flags().BoolVar(&serveProxyCmdConfig.localMode, "local-mode", false, "whether to serve extension list and extensions from a local directly or not")
	serveProxyCmd.Flags().StringVar(&serveProxyCmdConfig.outputDir, "output-dir", "downloaded-extensions", "output directory")
	serveProxyCmd.Flags().IntVar(&serveProxyCmdConfig.port, "port", 8080, "port to serve proxy on")
}
