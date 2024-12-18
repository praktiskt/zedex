package cmd

import (
	"encoding/json"
	"fmt"

	"zedex/zed"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var getExtensionIndexCmd = &cobra.Command{
	Use: "extension-index",
	Run: func(cmd *cobra.Command, args []string) {
		zc := zed.NewZedClient(1)
		extensions, err := zc.GetExtensionsIndex()
		if err != nil {
			log.Panic(err)
		}

		extensionsJson, err := json.MarshalIndent(extensions, "", "\t")
		if err != nil {
			log.Panic(err)
		}
		fmt.Println(string(extensionsJson))
	},
}

func init() {
	getCmd.AddCommand(getExtensionIndexCmd)
}
