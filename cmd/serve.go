package cmd

import (
	"github.com/spf13/cobra"
)

var serveCmd = &cobra.Command{
	Use: "serve",
}

func init() {
	rootCmd.AddCommand(serveCmd)
}
