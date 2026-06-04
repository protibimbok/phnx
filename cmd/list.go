package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/protibimbok/phnx/internal/config"
	"github.com/protibimbok/phnx/internal/fpm"
	"github.com/protibimbok/phnx/internal/nginx"
	"github.com/protibimbok/phnx/internal/php"
	"github.com/protibimbok/phnx/internal/ui"
	"github.com/spf13/cobra"
)

var listAll bool

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List registered sites",
	RunE:  runList,
}

func init() {
	rootCmd.AddCommand(listCmd)
	listCmd.Flags().BoolVar(&listAll, "all", false, "Include internal phnx-managed sites")
}

func runList(_ *cobra.Command, _ []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	var rows [][]string
	for _, site := range cfg.Sites {
		if site.Internal && !listAll {
			continue
		}

		url := cfg.SiteURL(site.Subdomain)
		if url == "" {
			url = fmt.Sprintf("http://%s.%s", site.Subdomain, cfg.TLD)
		}

		status := siteStatus(cfg, site)
		rows = append(rows, []string{
			site.Subdomain,
			url,
			site.Type,
			site.PHP,
			site.Path,
			status,
		})
	}

	if len(rows) == 0 {
		ui.Info("No sites registered. Run 'phnx init' to add one.")
		return nil
	}

	ui.PrintTable(
		[]string{"Subdomain", "URL", "Type", "PHP", "Path", "Status"},
		rows,
	)
	return nil
}

func siteStatus(cfg *config.Config, site config.Site) string {
	var issues []string

	// Check nginx config file
	confPath := nginx.SiteConfigPath(cfg.NginxSitesDir, site.Subdomain)
	if _, err := os.Stat(confPath); os.IsNotExist(err) {
		issues = append(issues, "no nginx config")
	}

	// Check FPM socket
	resolved, err := php.ResolvePHP(cfg, site.PHP)
	if err != nil || !fpm.SocketExists(resolved.Socket) {
		issues = append(issues, fmt.Sprintf("php%s-fpm not running", site.PHP))
	}

	// Check project directory
	if _, err := os.Stat(site.Path); os.IsNotExist(err) {
		issues = append(issues, "path missing")
	}

	if len(issues) == 0 {
		return ui.StatusHealthy()
	}
	return ui.StatusDegraded(strings.Join(issues, ", "))
}
