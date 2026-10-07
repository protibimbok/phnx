package initcmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/protibimbok/phnx/internal/config"
	"github.com/protibimbok/phnx/internal/database"
	"github.com/protibimbok/phnx/internal/ui"
)

func scaffoldWordPress(cwd string, cfg *config.Config, alongside bool) error {
	if alongside {
		ui.Info("Downloading WordPress (merging into existing directory)...")
	} else {
		ui.Info("Downloading WordPress...")
	}

	// Download latest WordPress
	wpCmd := exec.Command("bash", "-c",
		fmt.Sprintf(`cd %q && curl -O https://wordpress.org/latest.zip && unzip -oq latest.zip && cp -a wordpress/. . && rm -rf wordpress latest.zip`, cwd))
	wpCmd.Stdout = os.Stdout
	wpCmd.Stderr = os.Stderr
	if err := wpCmd.Run(); err != nil {
		return fmt.Errorf("downloading WordPress: %w", err)
	}

	dbName, err := ui.AskText("Database name", "wordpress", "wordpress")
	if err != nil {
		return err
	}

	if err := database.Create(cfg.MySQL.Host, cfg.MySQL.Port, cfg.MySQL.User, cfg.MySQL.Password, dbName); err != nil {
		ui.Warn(fmt.Sprintf("Could not create database %s: %v", dbName, err))
	} else {
		ui.Success(fmt.Sprintf("Database %s created", dbName))
	}

	// Run wp config create if wpcli available
	if _, err := exec.LookPath("wp"); err == nil {
		wpConfig := exec.Command("wp", "config", "create",
			fmt.Sprintf("--dbname=%s", dbName),
			fmt.Sprintf("--dbuser=%s", cfg.MySQL.User),
			fmt.Sprintf("--dbpass=%s", cfg.MySQL.Password),
			fmt.Sprintf("--dbhost=%s:%d", cfg.MySQL.Host, cfg.MySQL.Port),
		)
		wpConfig.Dir = cwd
		wpConfig.Stdout = os.Stdout
		wpConfig.Stderr = os.Stderr
		if err := wpConfig.Run(); err != nil {
			ui.Warn(fmt.Sprintf("wp config create failed: %v", err))
		} else {
			ui.Success("wp-config.php created")
		}
	}

	return nil
}
