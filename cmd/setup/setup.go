package setup

import (
	"github.com/spf13/cobra"
)

var SetupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Install helper tools (composer, wp-cli, database, phpmyadmin)",
}
