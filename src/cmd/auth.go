package cmd

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"github.com/spf13/cobra"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Tools for the authentication of users.",
}

var authSecretCmd = &cobra.Command{
	Use:   "secret",
	Short: "Generate a secret for the authentication configuration.",
	Args:  cobra.ExactArgs(0),
	Run: func(cmd *cobra.Command, args []string) {
		buf := make([]byte, 32)
		if _, err := rand.Read(buf); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "could not generate a secret: %v\n", err)
			return
		}
		fmt.Fprintln(cmd.OutOrStdout(), base64.RawURLEncoding.EncodeToString(buf))
	},
}

func init() {
	rootCmd.AddCommand(authCmd)
	authCmd.AddCommand(authSecretCmd)
}
