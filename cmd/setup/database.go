package setup

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/protibimbok/phnx/internal/config"
	"github.com/protibimbok/phnx/internal/system"
	"github.com/protibimbok/phnx/internal/ui"
)

// dbKind identifies which database engine is installed locally.
type dbKind int

const (
	dbNone dbKind = iota
	dbMariaDB
	dbMySQL
)

func (k dbKind) String() string {
	switch k {
	case dbMariaDB:
		return "MariaDB"
	case dbMySQL:
		return "MySQL"
	default:
		return "none"
	}
}

// detectDatabase reports which database engine is installed, if any. A dedicated
// `mariadb` binary means MariaDB; otherwise a bare `mysql` is disambiguated by
// its version banner (MariaDB ships a `mysql` compatibility symlink too).
func detectDatabase() dbKind {
	if _, err := exec.LookPath("mariadb"); err == nil {
		return dbMariaDB
	}
	mysqlPath, err := exec.LookPath("mysql")
	if err != nil {
		return dbNone
	}
	out, _ := exec.Command(mysqlPath, "--version").CombinedOutput()
	if strings.Contains(strings.ToLower(string(out)), "mariadb") {
		return dbMariaDB
	}
	return dbMySQL
}

// ensureDatabaseServer makes sure a MySQL/MariaDB server is installed, running,
// and has the phnx application user configured. It prompts the user for any
// credentials it needs rather than assuming passwordless access.
func ensureDatabaseServer(cfg *config.Config) error {
	ui.Header("Database server")

	kind := detectDatabase()
	if kind == dbNone {
		ui.Info("No MySQL or MariaDB server detected.")
		ok, err := ui.Confirm("Install MariaDB server now?", true)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("a MySQL or MariaDB server is required for phpMyAdmin")
		}
		if err := installMariaDB(); err != nil {
			return err
		}
		kind = dbMariaDB
	}
	ui.Success(fmt.Sprintf("Database engine: %s", kind))

	// Enable + start the service so we can connect to configure it.
	svc := system.NewServiceManager()
	serviceName := databaseServiceName(kind)
	if err := svc.Enable(serviceName); err != nil {
		ui.Warn(fmt.Sprintf("enabling %s: %v", serviceName, err))
	}
	if running, _ := svc.IsRunning(serviceName); !running {
		if err := svc.Start(serviceName); err != nil {
			return fmt.Errorf("starting %s: %w", serviceName, err)
		}
	}
	ui.Success(fmt.Sprintf("%s service is running", serviceName))

	return configureDatabaseUser(cfg)
}

// databaseServiceName maps the detected engine to its system service name.
func databaseServiceName(kind dbKind) string {
	if kind == dbMySQL {
		return "mysql"
	}
	return "mariadb"
}

// installMariaDB installs the MariaDB server package for the current platform.
func installMariaDB() error {
	ui.Info("Installing MariaDB server...")
	switch {
	case runtime.GOOS == "darwin":
		// brew must NOT run under sudo
		return system.RunUser("brew", "install", "mariadb")
	case system.IsArch():
		if err := system.Run("pacman", "-S", "--noconfirm", "mariadb"); err != nil {
			return err
		}
		// Arch ships an empty data dir — initialize it before first start.
		return system.Run("mariadb-install-db",
			"--user=mysql", "--basedir=/usr", "--datadir=/var/lib/mysql")
	case system.IsDebian():
		if err := system.Run("apt-get", "update"); err != nil {
			return err
		}
		return system.Run("apt-get", "install", "-y", "mariadb-server")
	case system.IsFedora():
		return system.Run("dnf", "install", "-y", "mariadb-server")
	default:
		return fmt.Errorf("unsupported platform for automated MariaDB install — install MariaDB manually and re-run")
	}
}

// configureDatabaseUser creates/updates the phnx application user that sites
// (and phpMyAdmin logins) connect with. It asks for the root password needed to
// run the setup SQL and for the application user's password.
func configureDatabaseUser(cfg *config.Config) error {
	ui.Info("Configuring database user...")

	// Credentials needed to connect as root and run the setup SQL. A blank
	// password means root authenticates over the unix socket (the default on a
	// fresh MariaDB install), in which case we connect as the root system user.
	rootPass, err := ui.AskPassword("Current database root password (leave blank if none / socket auth)")
	if err != nil {
		return err
	}

	user := cfg.MySQL.User
	if user == "" {
		user = "phnx"
	}
	pass, err := ui.AskText(
		fmt.Sprintf("Password for application user '%s' (leave blank for none)", user),
		"blank = no password",
		"",
	)
	if err != nil {
		return err
	}
	if pass == "" {
		ui.Warn(fmt.Sprintf("Application user '%s' will have an EMPTY password.", user))
		ui.Warn("This is fine for local development but must NEVER be used in production.")
	}

	if err := runRootSQL(rootPass, buildUserSQL(user, pass)); err != nil {
		return fmt.Errorf("configuring database user: %w", err)
	}

	cfg.MySQL.User = user
	cfg.MySQL.Password = pass
	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}
	ui.Success(fmt.Sprintf("Database user '%s' is ready", user))
	return nil
}

// buildUserSQL returns idempotent SQL that (re)creates the application user with
// full privileges. The user is granted from host '%' so phnx can reach it over
// TCP (127.0.0.1), which MySQL treats as a non-socket connection.
func buildUserSQL(user, pass string) string {
	return strings.Join([]string{
		fmt.Sprintf("CREATE USER IF NOT EXISTS '%s'@'%%' IDENTIFIED BY '%s';", user, pass),
		fmt.Sprintf("ALTER USER '%s'@'%%' IDENTIFIED BY '%s';", user, pass),
		fmt.Sprintf("GRANT ALL PRIVILEGES ON *.* TO '%s'@'%%' WITH GRANT OPTION;", user),
		"FLUSH PRIVILEGES;",
	}, "\n")
}

// runRootSQL pipes sql into a root mysql session. When no root password is given
// it relies on unix-socket auth, escalating with sudo on Linux when not already
// running as root.
func runRootSQL(rootPass, sql string) error {
	var name string
	var args []string
	switch {
	case rootPass != "":
		name, args = "mysql", []string{"-u", "root", "-p" + rootPass}
	case runtime.GOOS == "darwin", os.Getuid() == 0:
		name, args = "mysql", []string{"-u", "root"}
	default:
		name, args = "sudo", []string{"mysql", "-u", "root"}
	}

	cmd := exec.Command(name, args...)
	cmd.Stdin = strings.NewReader(sql)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w\n%s", err, strings.TrimSpace(buf.String()))
	}
	return nil
}
