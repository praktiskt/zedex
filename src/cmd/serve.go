package cmd

import (
	"fmt"

	"zedex/zed"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var serveCmdConfig = struct {
	localMode            bool
	outputDir            string
	port                 int
	hijackEditPrediction bool
	hijackLogin          bool
	hijackExtensionStore bool
	hijackReleases       bool
}{}

var serveCmd = &cobra.Command{
	Use:  "serve",
	Args: cobra.ExactArgs(0),
	Run: func(cmd *cobra.Command, args []string) {
		zc := zed.NewZedClient(1)
		zc.WithExtensionsLocalDir(serveCmdConfig.outputDir)
		api := zed.NewAPI(serveCmdConfig.localMode, zc, serveCmdConfig.port)

		log.Infof("serving on %v", serveCmdConfig.port)
		api.Router().Run(fmt.Sprintf(":%v", serveCmdConfig.port))
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)
	serveCmd.Flags().BoolVar(&serveCmdConfig.hijackLogin, "hijack-login", false, "hijack login requests, letting zedex manage them")
	serveCmd.Flags().BoolVar(&serveCmdConfig.hijackEditPrediction, "hijack-edit-prediction", false, "hijack edit prediction requests, letting zedex manage them")
	serveCmd.Flags().BoolVar(&serveCmdConfig.hijackExtensionStore, "hijack-extension-store", false, "hijack extension store requests, letting zedex manage them")
	serveCmd.Flags().BoolVar(&serveCmdConfig.hijackExtensionStore, "hijack-releases", false, "hijack release update requests, letting zedex manage them")
	serveCmd.Flags().BoolVar(&serveCmdConfig.hijackExtensionStore, "hijack-release-notes", false, "hijack release note requests, letting zedex manage them")
	serveCmd.Flags().BoolVar(&serveCmdConfig.localMode, "local-mode", false, "whether to serve extension list and extensions from a local directly or not")
	serveCmd.Flags().StringVar(&serveCmdConfig.outputDir, "output-dir", ".zedex-cache", "the directory where local artifacts (index and extensions) are located, ignored if local-mode=false")
	serveCmd.Flags().IntVar(&serveCmdConfig.port, "port", 8080, "port to serve proxy on")
}
