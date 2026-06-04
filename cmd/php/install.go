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

var installCmd = &cobra.Command{
	Use:   "install <version>",
	Short: "Install a PHP version",
	Args:  cobra.ExactArgs(1),
	RunE:  runInstall,
}

func init() {
	PHPCmd.AddCommand(installCmd)
}

func runInstall(_ *cobra.Command, args []string) error {
	version := args[0]
	if err := php.ValidateVersion(version); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	if cfg.HasPHPVersion(version) {
		ui.Info(fmt.Sprintf("PHP %s is already installed.", version))
		return nil
	}

	ui.Header(fmt.Sprintf("Installing PHP %s", version))

	// 1. Ensure PPA/tap
	if err := php.EnsurePPA(); err != nil {
		return err
	}

	// 2. Install packages
	if err := php.Install(version); err != nil {
		return err
	}
	ui.Success(fmt.Sprintf("PHP %s packages installed", version))

	// 3. Write FPM pool config
	if err := fpm.WritePool(version, cfg.RealUser, cfg.RealGroup); err != nil {
		return fmt.Errorf("writing FPM pool: %w", err)
	}
	ui.Success(fmt.Sprintf("FPM pool written for PHP %s", version))

	// 4. Enable and start FPM service
	svc := system.NewServiceManager()
	name := system.PHPFPMServiceName(version)
	if err := svc.Enable(name); err != nil {
		ui.Warn(fmt.Sprintf("enabling %s: %v", name, err))
	}
	if err := svc.Start(name); err != nil {
		return fmt.Errorf("starting %s: %w", name, err)
	}
	ui.Success(fmt.Sprintf("%s started", name))

	// 5. Update config
	cfg.AddPHPVersion(config.PHPVersion{Version: version, Tagged: true})
	if err := config.Save(cfg); err != nil {
		return err
	}

	ui.Success(fmt.Sprintf("PHP %s installed successfully", version))
	return nil
}
