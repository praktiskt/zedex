package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"zedex/utils"
	"zedex/zed"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var getLatestReleaseCmdConfig struct {
	outputDir string
}

var getLatestReleaseCmd = &cobra.Command{
	Use:   "latest-release",
	Short: "Get the latest release from zed.dev",
	Run: func(cmd *cobra.Command, args []string) {
		zc := zed.NewZedClient(1)
		latestRelease, err := zc.GetLatestZedVersion()
		if err != nil {
			log.Panic(err)
		}

		latestReleaseNotes, err := zc.GetLatestReleaseNotes()
		if err != nil {
			log.Panic(err)
		}

		latestReleaseJson, err := json.MarshalIndent(latestRelease, "", "\t")
		if err != nil {
			log.Panic(err)
		}

		latestReleaseNotesJson, err := json.MarshalIndent(latestReleaseNotes, "", "\t")
		if err != nil {
			log.Panic(err)
		}

		if getLatestReleaseCmdConfig.outputDir == "" {
			fmt.Println(string(latestReleaseJson))
			return
		}
		utils.CreateDirIfNotExists(getLatestReleaseCmdConfig.outputDir)
		latestReleasePath := getLatestReleaseCmdConfig.outputDir + "/latest_release.json"
		if err := os.WriteFile(latestReleasePath, latestReleaseJson, 0o644); err != nil {
			log.Panic(err)
		}

		latestReleaseNotePath := getLatestReleaseCmdConfig.outputDir + "/latest_release_notes.json"
		if err := os.WriteFile(latestReleaseNotePath, latestReleaseNotesJson, 0o644); err != nil {
			log.Panic(err)
		}
	},
}

func init() {
	getCmd.AddCommand(getLatestReleaseCmd)
	getLatestReleaseCmd.Flags().StringVar(&getLatestReleaseCmdConfig.outputDir, "output-dir", ".zedex-cache", "output directory of the 'latest_release.json' and 'latest_release_notes.json' file")
}
