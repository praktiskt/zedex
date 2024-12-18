package cmd

import (
	"fmt"

	"zedex/zed"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var serveCmdConfig = struct {
	localMode bool
	outputDir string
	port      int
}{}

var serveCmd = &cobra.Command{
	Use: "serve",
	Run: func(cmd *cobra.Command, args []string) {
		zc := zed.NewZedClient(1)
		zc.WithExtensionsLocalDir(serveCmdConfig.outputDir)
		api := zed.NewAPI(serveCmdConfig.localMode, zc)

		log.Infof("serving on %v", serveCmdConfig.port)
		api.Router().Run(fmt.Sprintf(":%v", serveCmdConfig.port))
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)
	serveCmd.Flags().BoolVar(&serveCmdConfig.localMode, "local-mode", false, "whether to serve extension list and extensions from a local directly or not")
	serveCmd.Flags().StringVar(&serveCmdConfig.outputDir, "output-dir", ".zedex-cache", "the directory where extensions will be downloaded and served from")
	serveCmd.Flags().IntVar(&serveCmdConfig.port, "port", 8080, "port to serve proxy on")
}
