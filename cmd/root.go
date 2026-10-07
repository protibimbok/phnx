package cmd

import (
	"fmt"
	"os"

	initcmd "github.com/protibimbok/phnx/cmd/init"
	phpcmd "github.com/protibimbok/phnx/cmd/php"
	"github.com/protibimbok/phnx/cmd/setup"
	"github.com/spf13/cobra"
)

var (
	Version = "dev"
	Commit  = "unknown"
)

var rootCmd = &cobra.Command{
	Use:   "phnx",
	Short: "PHP + Nginx local development environment manager",
	Long: `phnx manages nginx sites, PHP-FPM pools, and helper tools
for local PHP development environments.`,
	SilenceErrors: true,
	SilenceUsage:  true,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(initcmd.InitCmd)
	rootCmd.AddCommand(phpcmd.PHPCmd)
	rootCmd.AddCommand(setup.SetupCmd)

	rootCmd.Version = fmt.Sprintf("%s (%s)", Version, Commit)
}
