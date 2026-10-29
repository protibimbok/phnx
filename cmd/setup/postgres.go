package setup

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/protibimbok/phnx/internal/config"
	"github.com/protibimbok/phnx/internal/database"
	"github.com/protibimbok/phnx/internal/php"
	"github.com/protibimbok/phnx/internal/system"
	"github.com/protibimbok/phnx/internal/ui"
	"github.com/spf13/cobra"
)

// postgresBrewFormula is the Homebrew formula installed when no PostgreSQL
// server is present on macOS.
const postgresBrewFormula = "postgresql@17"

var postgresCmd = &cobra.Command{
	Use:     "postgres",
	Aliases: []string{"postgresql", "pgsql"},
	Short:   "Install and configure PostgreSQL for local development",
	RunE:    runPostgres,
}

func init() {
	SetupCmd.AddCommand(postgresCmd)
}

func runPostgres(_ *cobra.Command, _ []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	return ensurePostgresServer(cfg)
}

// ensurePostgresServer makes sure a PostgreSQL server is installed, initialized,
// running, and has the phnx application role configured.
func ensurePostgresServer(cfg *config.Config) error {
	ui.Header("PostgreSQL server")

	service := detectPostgres()
	if service == "" {
		ui.Info("No PostgreSQL server detected.")
		ok, err := ui.Confirm("Install PostgreSQL server now?", true)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("a PostgreSQL server is required")
		}
		if err := installPostgres(); err != nil {
			return err
		}
		if service = detectPostgres(); service == "" {
			return fmt.Errorf("PostgreSQL was installed but its server could not be found")
		}
	}
	ui.Success("PostgreSQL server found")

	if err := initPostgresDataDir(); err != nil {
		return err
	}

	svc := system.NewServiceManager()
	if err := svc.Enable(service); err != nil {
		ui.Warn(fmt.Sprintf("enabling %s: %v", service, err))
	}
	if running, _ := svc.IsRunning(service); !running {
		if err := svc.Start(service); err != nil {
			return fmt.Errorf("starting %s: %w", service, err)
		}
	}
	ui.Success(fmt.Sprintf("%s service is running", service))

	if runtime.GOOS != "darwin" {
		if err := enablePostgresPasswordAuth(); err != nil {
			ui.Warn(fmt.Sprintf("Could not update pg_hba.conf: %v", err))
			ui.Warn("If password logins over 127.0.0.1 fail, switch 'ident' to 'scram-sha-256' in pg_hba.conf.")
		}
	}

	if err := configurePostgresRole(cfg); err != nil {
		return err
	}
	warnMissingPgsqlExtension(cfg)
	return nil
}

// detectPostgres returns the service name of the installed PostgreSQL server,
// or "" when none is installed. Client-only installs (just psql) don't count.
func detectPostgres() string {
	if runtime.GOOS == "darwin" {
		out, err := exec.Command("brew", "list", "--formula").Output()
		if err != nil {
			return ""
		}
		var formulae []string
		for f := range strings.FieldsSeq(string(out)) {
			if f == "postgresql" || strings.HasPrefix(f, "postgresql@") {
				formulae = append(formulae, f)
			}
		}
		if len(formulae) == 0 {
			return ""
		}
		sort.Strings(formulae)
		return formulae[len(formulae)-1]
	}

	if _, err := exec.LookPath("postgres"); err == nil {
		return "postgresql"
	}
	// Debian keeps server binaries in a versioned directory off PATH.
	if matches, _ := filepath.Glob("/usr/lib/postgresql/*/bin/postgres"); len(matches) > 0 {
		return "postgresql"
	}
	if _, err := os.Stat("/usr/bin/postgres"); err == nil {
		return "postgresql"
	}
	return ""
}

// installPostgres installs the PostgreSQL server package for the current platform.
func installPostgres() error {
	ui.Info("Installing PostgreSQL server...")
	switch {
	case runtime.GOOS == "darwin":
		// brew must NOT run under sudo
		return system.RunUser("brew", "install", postgresBrewFormula)
	case system.IsArch():
		return system.Run("pacman", "-S", "--noconfirm", "--needed", "postgresql")
	case system.IsDebian():
		if err := system.Run("apt-get", "update"); err != nil {
			return err
		}
		return system.Run("apt-get", "install", "-y", "postgresql")
	case system.IsFedora():
		return system.Run("dnf", "install", "-y", "postgresql-server")
	default:
		return fmt.Errorf("unsupported platform for automated PostgreSQL install — install PostgreSQL manually and re-run")
	}
}

// initPostgresDataDir initializes the cluster on distros whose packages ship an
// empty data directory (Arch, Fedora). Debian and Homebrew initialize on install.
func initPostgresDataDir() error {
	var dataDir string
	switch {
	case runtime.GOOS == "darwin":
		return nil
	case system.IsArch():
		dataDir = "/var/lib/postgres/data"
	case system.IsFedora():
		dataDir = "/var/lib/pgsql/data"
	default:
		return nil
	}

	// The data dir is only readable by the postgres user, so check via sudo.
	if _, err := system.Output("test", "-f", filepath.Join(dataDir, "PG_VERSION")); err == nil {
		return nil
	}

	ui.Info(fmt.Sprintf("Initializing PostgreSQL data directory at %s...", dataDir))
	if system.IsFedora() {
		return system.Run("postgresql-setup", "--initdb")
	}
	return system.Run("runuser", "-u", "postgres", "--",
		"initdb",
		"--locale=C.UTF-8", "--encoding=UTF8",
		"--auth-local=peer", "--auth-host=scram-sha-256",
		"-D", dataDir)
}

var hbaIdentRe = regexp.MustCompile(`(\s)ident(\s|$)`)

// enablePostgresPasswordAuth switches TCP ("host") rules that use ident auth —
// Fedora's default — to scram-sha-256 so sites can log in with a password over
// 127.0.0.1, then reloads the server config.
func enablePostgresPasswordAuth() error {
	hbaFile, err := runPostgresSuperuserSQL("SHOW hba_file;")
	if err != nil {
		return err
	}
	hbaFile = strings.TrimSpace(hbaFile)

	content, err := system.Output("cat", hbaFile)
	if err != nil {
		return err
	}

	lines := strings.Split(content, "\n")
	changed := false
	for i, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 5 || !strings.HasPrefix(fields[0], "host") || fields[4] != "ident" {
			continue
		}
		lines[i] = hbaIdentRe.ReplaceAllString(line, "${1}scram-sha-256${2}")
		changed = true
	}
	if !changed {
		return nil
	}

	if err := system.WriteFile(hbaFile, strings.Join(lines, "\n")); err != nil {
		return err
	}
	if _, err := runPostgresSuperuserSQL("SELECT pg_reload_conf();"); err != nil {
		return err
	}
	ui.Success("Enabled password authentication for TCP connections in pg_hba.conf")
	return nil
}

// configurePostgresRole creates/updates the phnx application role as a
// superuser so it can create databases for sites.
func configurePostgresRole(cfg *config.Config) error {
	ui.Info("Configuring PostgreSQL role...")

	user := cfg.Postgres.User
	if user == "" {
		user = "phnx"
	}
	pass, err := ui.AskText(
		fmt.Sprintf("Database password for the phnx PostgreSQL role '%s' — leave blank for 'secret'", user),
		"database password for sites",
		"secret",
	)
	if err != nil {
		return err
	}

	if _, err := runPostgresSuperuserSQL(buildPostgresRoleSQL(user, pass)); err != nil {
		return fmt.Errorf("configuring PostgreSQL role: %w", err)
	}

	if cfg.Postgres.Host == "" {
		cfg.Postgres.Host = "127.0.0.1"
	}
	if cfg.Postgres.Port == 0 {
		cfg.Postgres.Port = 5432
	}
	cfg.Postgres.User = user
	cfg.Postgres.Password = pass
	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}
	ui.Success(fmt.Sprintf("PostgreSQL role '%s' is ready", user))
	return nil
}

// buildPostgresRoleSQL returns idempotent SQL that (re)creates the application
// role as a login superuser with the given password.
func buildPostgresRoleSQL(user, pass string) string {
	ident := database.QuotePgIdent(user)
	return fmt.Sprintf(`DO $phnx$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = %s) THEN
    CREATE ROLE %s;
  END IF;
END
$phnx$;
ALTER ROLE %s WITH LOGIN SUPERUSER PASSWORD %s;
`, database.QuotePgLiteral(user), ident, ident, database.QuotePgLiteral(pass))
}

// runPostgresSuperuserSQL pipes sql into a psql session as the database
// superuser and returns its unaligned output. On Linux that is the postgres
// system user (peer auth); Homebrew makes the installing user a superuser.
func runPostgresSuperuserSQL(sql string) (string, error) {
	return runPostgresSuperuserSQLIn("postgres", sql)
}

// runPostgresSuperuserSQLIn is runPostgresSuperuserSQL against database db.
func runPostgresSuperuserSQLIn(db, sql string) (string, error) {
	psql, err := database.PsqlBinary()
	if err != nil {
		return "", err
	}
	psqlArgs := []string{psql, "-d", db, "-tAX", "-v", "ON_ERROR_STOP=1"}

	var args []string
	switch {
	case runtime.GOOS == "darwin":
		args = psqlArgs
	case os.Getuid() == 0:
		args = append([]string{"runuser", "-u", "postgres", "--"}, psqlArgs...)
	default:
		args = append([]string{"sudo", "-u", "postgres"}, psqlArgs...)
	}

	cmd := exec.Command(args[0], args[1:]...)
	// The postgres user usually can't read the caller's cwd; avoid psql's warning.
	cmd.Dir = "/"
	cmd.Stdin = strings.NewReader(sql)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w\n%s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// warnMissingPgsqlExtension hints at installing pdo_pgsql when the default PHP
// can't talk to PostgreSQL yet.
func warnMissingPgsqlExtension(cfg *config.Config) {
	if cfg.DefaultPHP == "" {
		return
	}
	resolved, err := php.ResolvePHP(cfg, cfg.DefaultPHP)
	if err != nil || php.IsLoaded(resolved.Binary, "pdo_pgsql") {
		return
	}
	ui.Warn(fmt.Sprintf("PHP %s does not have the pdo_pgsql extension loaded.", cfg.DefaultPHP))
	ui.Warn("Run `phnx php ext install pgsql` so your sites can connect to PostgreSQL.")
}
