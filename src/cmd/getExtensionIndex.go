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

var getExtensionIndexCmdConfig = struct {
	outputDir string
}{}

var getExtensionIndexCmd = &cobra.Command{
	Use:    "extension-index",
	PreRun: func(cmd *cobra.Command, args []string) { manageDefaultFlags() },
	Run: func(cmd *cobra.Command, args []string) {
		zc := zed.NewZedClient(1)
		extensions, err := zc.GetExtensionsIndex()
		if err != nil {
			log.Panic(err)
		}

		wrapped := extensions.AsWrapped()
		extensionsJson, err := json.MarshalIndent(wrapped, "", "\t")
		if err != nil {
			log.Panic(err)
		}
		if getExtensionIndexCmdConfig.outputDir == "" {
			fmt.Println(string(extensionsJson))
		} else {
			utils.CreateDirIfNotExists(getExtensionIndexCmdConfig.outputDir)
			extensionsFilePath := getExtensionIndexCmdConfig.outputDir + "/extensions.json"
			err := os.WriteFile(extensionsFilePath, extensionsJson, 0o644)
			if err != nil {
				log.Panic(err)
			}
		}
	},
}

func init() {
	getCmd.AddCommand(getExtensionIndexCmd)
	getExtensionIndexCmd.Flags().StringVar(&getExtensionIndexCmdConfig.outputDir, "output-dir", ".zedex-cache", "output directory of the 'extensions.json' file")
}
