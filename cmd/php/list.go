package phpcmd

import (
	"fmt"

	"github.com/protibimbok/phnx/internal/config"
	"github.com/protibimbok/phnx/internal/fpm"
	"github.com/protibimbok/phnx/internal/php"
	"github.com/protibimbok/phnx/internal/system"
	"github.com/protibimbok/phnx/internal/ui"
	"github.com/spf13/cobra"
)

var phpListCmd = &cobra.Command{
	Use:   "list",
	Short: "List PHP versions",
	RunE:  runPHPList,
}

func init() {
	PHPCmd.AddCommand(phpListCmd)
}

func runPHPList(_ *cobra.Command, _ []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	installed, _ := php.DetectInstalled()
	installedSet := make(map[string]bool)
	for _, v := range installed {
		installedSet[v] = true
	}

	// Merge config versions with detected versions (keyed by version string).
	allVersions := make(map[string]bool)
	for _, e := range cfg.PHPVersions {
		allVersions[e.Version] = true
	}
	for _, v := range installed {
		allVersions[v] = true
	}

	svc := system.NewServiceManager()
	var rows [][]string
	for version := range allVersions {
		status := "not installed"
		if installedSet[version] {
			status = "installed"
		}

		fpmStatus := "stopped"
		resolved, resolveErr := php.ResolvePHP(cfg, version)
		if resolveErr == nil {
			if running, err := svc.IsRunning(resolved.Service); err == nil && running {
				fpmStatus = "running"
			} else if fpm.SocketExists(resolved.Socket) {
				fpmStatus = "running"
			}
		}

		siteCount := 0
		for _, s := range cfg.Sites {
			if s.PHP == version {
				siteCount++
			}
		}

		defaultMark := ""
		if version == cfg.DefaultPHP {
			defaultMark = "★"
		}

		rows = append(rows, []string{
			version,
			status,
			fpmStatus,
			fmt.Sprintf("%d", siteCount),
			defaultMark,
		})
	}

	if len(rows) == 0 {
		ui.Info("No PHP versions found. Run 'phnx php install <version>' to install one.")
		return nil
	}

	ui.PrintTable(
		[]string{"Version", "Status", "FPM", "Sites", "Default"},
		rows,
	)
	return nil
}
