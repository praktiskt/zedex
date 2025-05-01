package cmd

import (
	"fmt"

	"zedex/zed"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var serveCmdConfig = struct {
	outputDir            string
	port                 int
	enableEditPrediction bool
	enableLogin          bool
	enableExtensionStore bool
	enableReleases       bool
	enableReleaseNotes   bool
}{}

var serveCmd = &cobra.Command{
	Use:  "serve",
	Args: cobra.ExactArgs(0),
	Run: func(cmd *cobra.Command, args []string) {
		if !serveCmdConfig.enableLogin {
			log.Fatalf("zedex does not support login forwarding yet")
		}
		if !serveCmdConfig.enableEditPrediction {
			log.Fatalf("zedex does not support edit prediction forwarding yet")
		}

		zc := zed.NewZedClient(1)
		zc.WithExtensionsLocalDir(serveCmdConfig.outputDir)
		api := zed.NewAPI(
			serveCmdConfig.enableExtensionStore,
			serveCmdConfig.enableLogin,
			serveCmdConfig.enableEditPrediction,
			serveCmdConfig.enableReleases,
			serveCmdConfig.enableReleaseNotes,
			zc,
			serveCmdConfig.port)

		log.Infof("serving on %v", serveCmdConfig.port)
		api.Router().Run(fmt.Sprintf(":%v", serveCmdConfig.port))
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)
	serveCmd.Flags().BoolVar(&serveCmdConfig.enableLogin, "enable-login", true, "enable login requests, letting zedex manage them")
	serveCmd.Flags().BoolVar(&serveCmdConfig.enableEditPrediction, "enable-edit-prediction", true, "enable edit prediction requests, letting zedex manage them")
	serveCmd.Flags().BoolVar(&serveCmdConfig.enableExtensionStore, "enable-extension-store", true, "enable extension store requests, letting zedex manage them")
	serveCmd.Flags().BoolVar(&serveCmdConfig.enableReleases, "enable-releases", true, "enable release update requests, letting zedex manage them")
	serveCmd.Flags().BoolVar(&serveCmdConfig.enableReleaseNotes, "enable-release-notes", true, "enable release note requests, letting zedex manage them")
	serveCmd.Flags().StringVar(&serveCmdConfig.outputDir, "output-dir", ".zedex-cache", "the directory where local artifacts (index and extensions) are located, ignored if local-mode=false")
	serveCmd.Flags().IntVar(&serveCmdConfig.port, "port", 8080, "port to serve proxy on")
}
