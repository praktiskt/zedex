package cmd

import (
	"fmt"
	"os"

	"zedex/zed"
	"zedex/zed/auth"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var serveCmdConfig = struct {
	outputDir            string
	port                 int
	enableLogin          bool
	enableExtensionStore bool
	enableReleases       bool
	enableReleaseNotes   bool
	authConfig           string
}{}

var serveCmd = &cobra.Command{
	Use:    "serve",
	Args:   cobra.ExactArgs(0),
	PreRun: func(cmd *cobra.Command, args []string) { manageDefaultFlags() },
	Run: func(cmd *cobra.Command, args []string) {
		if !serveCmdConfig.enableLogin {
			log.Fatalf("zedex does not support login forwarding yet")
		}

		zc := zed.NewZedClient(1)
		zc.WithExtensionsLocalDir(serveCmdConfig.outputDir)

		var manager *auth.Manager
		if path := authConfigPath(); path != "" {
			cfg, err := auth.LoadConfig(path)
			if err != nil {
				log.Fatalf("could not load the auth config: %v", err)
			}
			manager, err = auth.New(cfg)
			if err != nil {
				log.Fatalf("could not set up authentication: %v", err)
			}
			log.Infof("authentication is on, %d provider(s) configured", len(cfg.Providers))
		}

		api := zed.NewAPI(zed.Options{
			EnableExtensionStore: serveCmdConfig.enableExtensionStore,
			EnableLogin:          serveCmdConfig.enableLogin,
			EnableReleases:       serveCmdConfig.enableReleases,
			EnableReleaseNotes:   serveCmdConfig.enableReleaseNotes,
			Client:               zc,
			Port:                 serveCmdConfig.port,
			Auth:                 manager,
		})

		log.Infof("serving on %v", serveCmdConfig.port)
		api.Router().Run(fmt.Sprintf(":%v", serveCmdConfig.port))
	},
}

// authConfigPath returns the path of the authentication configuration, if
// there is one.
func authConfigPath() string {
	if serveCmdConfig.authConfig != "" {
		return serveCmdConfig.authConfig
	}
	return os.Getenv("ZEDEX_AUTH_CONFIG")
}

func init() {
	rootCmd.AddCommand(serveCmd)
	serveCmd.Flags().BoolVar(&serveCmdConfig.enableLogin, "enable-login", true, "enable login requests, letting zedex manage them")
	serveCmd.Flags().BoolVar(&serveCmdConfig.enableExtensionStore, "enable-extension-store", true, "enable extension store requests, letting zedex manage them")
	serveCmd.Flags().BoolVar(&serveCmdConfig.enableReleases, "enable-releases", true, "enable release update requests, letting zedex manage them")
	serveCmd.Flags().BoolVar(&serveCmdConfig.enableReleaseNotes, "enable-release-notes", true, "enable release note requests, letting zedex manage them")
	serveCmd.Flags().StringVar(&serveCmdConfig.authConfig, "auth-config", "", "path of the authentication configuration, defaults to the ZEDEX_AUTH_CONFIG environment variable")
	serveCmd.Flags().StringVar(&serveCmdConfig.outputDir, "output-dir", ".zedex-cache", "the directory where local artifacts (index and extensions) are located, ignored if local-mode=false")
	serveCmd.Flags().IntVar(&serveCmdConfig.port, "port", 8080, "port to serve proxy on")
}
