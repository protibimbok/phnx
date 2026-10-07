package initcmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/protibimbok/phnx/internal/config"
	"github.com/protibimbok/phnx/internal/database"
	"github.com/protibimbok/phnx/internal/ui"
)

func maybeConfigureLaravelDatabase(cwd, subdomain, phpBinary string, cfg *config.Config) error {
	if !detectLaravel(cwd) {
		return nil
	}

	ok, err := ui.Confirm("Create a database and configure Laravel to use it?", true)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	defaultName := database.SanitizeName(subdomain)
	dbName, err := ui.AskText("Database name", defaultName, defaultName)
	if err != nil {
		return err
	}
	dbName = database.SanitizeName(dbName)

	if err := database.Create(cfg.MySQL.Host, cfg.MySQL.Port, cfg.MySQL.User, cfg.MySQL.Password, dbName); err != nil {
		ui.Warn(fmt.Sprintf("Could not create database %s: %v", dbName, err))
		ui.Warn("Run `phnx setup database` to install and configure the database server, then create it manually.")
	} else {
		ui.Success(fmt.Sprintf("Database %s created", dbName))
	}

	envPath, err := ensureEnvFile(cwd)
	if err != nil {
		ui.Warn(err.Error())
		return nil
	}

	if err := writeEnvValues(envPath, []envPair{
		{"DB_CONNECTION", "mysql"},
		{"DB_HOST", cfg.MySQL.Host},
		{"DB_PORT", fmt.Sprintf("%d", cfg.MySQL.Port)},
		{"DB_DATABASE", dbName},
		{"DB_USERNAME", cfg.MySQL.User},
		{"DB_PASSWORD", cfg.MySQL.Password},
	}); err != nil {
		return fmt.Errorf("updating .env: %w", err)
	}
	ui.Success("Database settings written to .env")

	if envValue(envPath, "APP_KEY") == "" {
		if err := runArtisan(cwd, phpBinary, "key:generate", "--ansi"); err != nil {
			ui.Warn(fmt.Sprintf("php artisan key:generate failed: %v", err))
		} else {
			ui.Success("Application key generated")
		}
	}
	return nil
}

func ensureEnvFile(cwd string) (string, error) {
	envPath := filepath.Join(cwd, ".env")
	if _, err := os.Stat(envPath); err == nil {
		return envPath, nil
	}
	example, err := os.ReadFile(filepath.Join(cwd, ".env.example"))
	if err != nil {
		return "", fmt.Errorf("no .env or .env.example found in %s — skipping Laravel environment setup", cwd)
	}
	if err := os.WriteFile(envPath, example, 0600); err != nil {
		return "", fmt.Errorf("creating .env: %w", err)
	}
	ui.Success(".env created from .env.example")
	return envPath, nil
}

type envPair struct {
	Key   string
	Value string
}

func writeEnvValues(path string, pairs []envPair) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")

	for _, p := range pairs {
		re := regexp.MustCompile(`^\s*#?\s*` + regexp.QuoteMeta(p.Key) + `\s*=`)
		newLine := p.Key + "=" + quoteEnvValue(p.Value)
		replaced := false
		for i, line := range lines {
			if re.MatchString(line) {
				lines[i] = newLine
				replaced = true
				break
			}
		}
		if !replaced {
			lines = appendEnvLine(lines, newLine)
		}
	}

	out := strings.Join(lines, "\n")
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return os.WriteFile(path, []byte(out), 0600)
}

func appendEnvLine(lines []string, line string) []string {
	if n := len(lines); n > 0 && lines[n-1] == "" {
		return append(lines[:n-1], line, "")
	}
	return append(lines, line)
}

func quoteEnvValue(v string) string {
	if v == "" {
		return ""
	}
	safe := true
	for _, r := range v {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.' || r == '/' || r == ':' || r == '@') {
			safe = false
			break
		}
	}
	if safe {
		return v
	}
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, `"`, `\"`)
	return `"` + v + `"`
}

func envValue(path, key string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	prefix := key + "="
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			return strings.Trim(strings.TrimPrefix(line, prefix), `"'`)
		}
	}
	return ""
}

func runArtisan(cwd, phpBinary string, args ...string) error {
	if phpBinary == "" {
		phpBinary = "php"
	}
	if _, err := os.Stat(phpBinary); err != nil && !filepath.IsAbs(phpBinary) {
		if p, lookErr := exec.LookPath(phpBinary); lookErr == nil {
			phpBinary = p
		}
	}
	cmd := exec.Command(phpBinary, append([]string{"artisan"}, args...)...)
	cmd.Dir = cwd
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
