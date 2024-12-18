package cmd

import (
	"os"
	"path"

	"zedex/utils"
	"zedex/zed"

	"github.com/remeh/sizedwaitgroup"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var getExtensionCmdConfig = struct {
	outputDir string
}{}

var getExtensionCmd = &cobra.Command{
	Use: "extension",
	Run: func(cmd *cobra.Command, args []string) {
		zc := zed.NewZedClient(1)
		swg := sizedwaitgroup.New(20)
		for _, id := range args {
			swg.Add()
			go func() {
				defer swg.Done()
				log.Infof("(extension=%v) downloading", id)
				bytes, err := zc.DownloadExtensionArchiveDefault(zed.Extension{ID: id})
				if err != nil {
					log.Errorf("(extension=%v) %v", err.Error())
					return
				}

				utils.CreateDirIfNotExists(getExtensionCmdConfig.outputDir)
				err = os.WriteFile(path.Join(getExtensionCmdConfig.outputDir, id+".tar.gz"), bytes, 0o644)
				if err != nil {
					log.Errorf("(extension=%v) %v", err.Error())
					return
				}
				log.Infof("(extension=%v) wrote %v bytes", id, len(bytes))
			}()
		}
		swg.Wait()
	},
}

func init() {
	getCmd.AddCommand(getExtensionCmd)
	getExtensionCmd.Flags().StringVar(&getExtensionCmdConfig.outputDir, "output-dir", ".zedex-cache", "output directory")
}
