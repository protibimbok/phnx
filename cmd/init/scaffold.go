package initcmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/protibimbok/phnx/internal/config"
	"github.com/protibimbok/phnx/internal/ui"
)

const (
	scaffoldErase     = "Erase directory and install fresh"
	scaffoldAlongside = "Install alongside (overwrite conflicting files)"
	scaffoldSkip      = "Skip scaffolding"
)

func maybeScaffold(siteType, cwd string, cfg *config.Config) error {
	switch siteType {
	case "laravel":
		return maybeScaffoldLaravel(cwd)
	case "wordpress":
		return maybeScaffoldWordPress(cwd, cfg)
	default:
		return nil
	}
}

func maybeScaffoldLaravel(cwd string) error {
	if detectLaravel(cwd) {
		ui.Info("Existing Laravel project detected, skipping scaffolding")
		return nil
	}

	if isDirEmpty(cwd) {
		scaffold, err := ui.Confirm("Directory is empty. Create a new Laravel project?", true)
		if err != nil {
			return err
		}
		if !scaffold {
			return nil
		}
		return runLaravelCreate(cwd, false)
	}

	action, err := ui.AskSelect(
		"Directory is not empty and does not look like Laravel. What would you like to do?",
		[]string{scaffoldErase, scaffoldAlongside, scaffoldSkip},
	)
	if err != nil {
		return err
	}
	return applyScaffoldAction(action, cwd, runLaravelCreate)
}

func maybeScaffoldWordPress(cwd string, cfg *config.Config) error {
	if detectWordPress(cwd) {
		ui.Info("Existing WordPress installation detected, skipping scaffolding")
		return nil
	}

	if isDirEmpty(cwd) {
		return scaffoldWordPress(cwd, cfg, false)
	}

	action, err := ui.AskSelect(
		"Directory is not empty and does not look like WordPress. What would you like to do?",
		[]string{scaffoldErase, scaffoldAlongside, scaffoldSkip},
	)
	if err != nil {
		return err
	}
	switch action {
	case scaffoldErase:
		if err := clearDirectory(cwd); err != nil {
			return fmt.Errorf("clearing directory: %w", err)
		}
		return scaffoldWordPress(cwd, cfg, false)
	case scaffoldAlongside:
		return scaffoldWordPress(cwd, cfg, true)
	default:
		return nil
	}
}

type scaffoldFn func(cwd string, alongside bool) error

func applyScaffoldAction(action, cwd string, scaffold scaffoldFn) error {
	switch action {
	case scaffoldErase:
		if err := clearDirectory(cwd); err != nil {
			return fmt.Errorf("clearing directory: %w", err)
		}
		return scaffold(cwd, false)
	case scaffoldAlongside:
		return scaffold(cwd, true)
	default:
		return nil
	}
}

func detectLaravel(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, "artisan")); err != nil {
		return false
	}
	data, err := os.ReadFile(filepath.Join(dir, "composer.json"))
	if err != nil {
		return true
	}
	content := string(data)
	return strings.Contains(content, "laravel/framework") ||
		strings.Contains(content, "laravel/laravel")
}

func detectWordPress(dir string) bool {
	for _, name := range []string{"wp-load.php", "wp-config.php"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	_, errAdmin := os.Stat(filepath.Join(dir, "wp-admin"))
	_, errIncludes := os.Stat(filepath.Join(dir, "wp-includes"))
	return errAdmin == nil && errIncludes == nil
}

func clearDirectory(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func runLaravelCreate(cwd string, alongside bool) error {
	ui.Info("Running composer create-project laravel/laravel ...")

	if alongside {
		tmpDir, err := os.MkdirTemp("", "phnx-laravel-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmpDir)

		cmd := exec.Command("composer", "create-project", "laravel/laravel", tmpDir)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("composer create-project: %w", err)
		}
		return copyDirContents(tmpDir, cwd)
	}

	cmd := exec.Command("composer", "create-project", "laravel/laravel", ".")
	cmd.Dir = cwd
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("composer create-project: %w", err)
	}
	return nil
}

func copyDirContents(src, dst string) error {
	srcArg := filepath.Join(src, ".")
	dstArg := dst + string(filepath.Separator)
	cmd := exec.Command("cp", "-a", srcArg, dstArg)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("copying scaffolded files: %w", err)
	}
	return nil
}

func isDirEmpty(path string) bool {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	return len(entries) == 0
}
