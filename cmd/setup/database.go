package setup

import (
	"github.com/protibimbok/phnx/internal/config"
	"github.com/protibimbok/phnx/internal/ui"
	"github.com/spf13/cobra"
)

const (
	dbChoiceMariaDB  = "MySQL/MariaDB"
	dbChoicePostgres = "PostgreSQL"
)

var databaseCmd = &cobra.Command{
	Use:   "database",
	Short: "Install and configure MySQL/MariaDB and/or PostgreSQL",
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
	choices, err := ui.AskMultiSelect("Which database servers do you want to set up?",
		[]string{dbChoiceMariaDB, dbChoicePostgres})
	if err != nil {
		return err
	}
	for _, c := range choices {
		switch c {
		case dbChoiceMariaDB:
			err = ensureDatabaseServer(cfg)
		case dbChoicePostgres:
			err = ensurePostgresServer(cfg)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
