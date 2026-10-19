package setup

import (
	"github.com/protibimbok/phnx/internal/config"
	"github.com/spf13/cobra"
)

var databaseCmd = &cobra.Command{
	Use:   "database",
	Short: "Install and configure MySQL/MariaDB for local development",
	RunE:  runDatabase,
}

func init() {
	SetupCmd.AddCommand(databaseCmd)
}

func runDatabase(_ *cobra.Command, _ []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	return ensureDatabaseServer(cfg)
}
