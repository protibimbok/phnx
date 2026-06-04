package cmd

import (
	"fmt"
	"os"

	"github.com/protibimbok/phnx/internal/config"
	"github.com/protibimbok/phnx/internal/hosts"
	"github.com/protibimbok/phnx/internal/nginx"
	"github.com/protibimbok/phnx/internal/ui"
	"github.com/spf13/cobra"
)

var removeCmd = &cobra.Command{
	Use:   "remove [subdomain]",
	Short: "Remove a registered site",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runRemove,
}

func init() {
	rootCmd.AddCommand(removeCmd)
}

func runRemove(_ *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Resolve target: arg or match by cwd path
	var site *config.Site
	if len(args) > 0 {
		site = cfg.FindSite(args[0])
		if site == nil {
			return fmt.Errorf("site %q not found", args[0])
		}
	} else {
		cwd, _ := os.Getwd()
		site = cfg.FindSiteByPath(cwd)
		if site == nil {
			return fmt.Errorf("no phnx site registered for current directory — pass a subdomain as argument")
		}
	}

	// Show confirmation
	domain := site.Subdomain + "." + cfg.TLD
	ui.Header(fmt.Sprintf("Remove site: %s", domain))
	ui.Info(fmt.Sprintf("Path:    %s", site.Path))
	ui.Info(fmt.Sprintf("Type:    %s", site.Type))
	ui.Info(fmt.Sprintf("PHP:     %s", site.PHP))

	ok, err := ui.Confirm(fmt.Sprintf("Remove %s?", domain), false)
	if err != nil {
		return err
	}
	if !ok {
		ui.Info("Aborted.")
		return nil
	}

	// Remove nginx config
	if err := nginx.RemoveSiteConfig(cfg.NginxSitesDir, site.Subdomain); err != nil {
		ui.Warn(fmt.Sprintf("removing nginx config: %v", err))
	} else {
		ui.Success("nginx config removed")
	}

	// Remove /etc/hosts entry
	if err := hosts.Remove(domain); err != nil {
		ui.Warn(fmt.Sprintf("removing /etc/hosts entry: %v", err))
	} else {
		ui.Success("/etc/hosts entry removed")
	}

	// Reload nginx
	if err := nginx.Reload(); err != nil {
		ui.Warn(fmt.Sprintf("nginx reload: %v", err))
	} else {
		ui.Success("nginx reloaded")
	}

	// Remove from config
	cfg.RemoveSite(site.Subdomain)
	if err := config.Save(cfg); err != nil {
		return err
	}

	// Optionally remove log files
	removeLogs, _ := ui.Confirm("Remove nginx log files for this site?", false)
	if removeLogs {
		if err := RemoveLogFiles(site.Subdomain); err != nil {
			ui.Warn(fmt.Sprintf("removing logs: %v", err))
		} else {
			ui.Success("Log files removed")
		}
	}

	// Optionally delete project directory
	deleteDir, _ := ui.Confirm(fmt.Sprintf("Delete project directory %s?", site.Path), false)
	if deleteDir {
		if err := os.RemoveAll(site.Path); err != nil {
			ui.Warn(fmt.Sprintf("deleting project dir: %v", err))
		} else {
			ui.Success(fmt.Sprintf("Deleted %s", site.Path))
		}
	}

	ui.Separator()
	ui.Success(fmt.Sprintf("Site %s removed.", domain))
	return nil
}
