package setup

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/protibimbok/phnx/internal/config"
	"github.com/protibimbok/phnx/internal/database"
	"github.com/protibimbok/phnx/internal/system"
	"github.com/protibimbok/phnx/internal/ui"
	"github.com/spf13/cobra"
)

const pldebuggerSourceURL = "https://github.com/EnterpriseDB/pldebugger/archive/refs/heads/master.tar.gz"

var pgdebuggerCmd = &cobra.Command{
	Use:   "pgdebugger",
	Short: "Install the PL/pgSQL debugger (pldebugger) used by phpPgAdmin",
	RunE:  runPgDebugger,
}

func init() {
	SetupCmd.AddCommand(pgdebuggerCmd)
}

func runPgDebugger(_ *cobra.Command, _ []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := ensurePostgresServer(cfg); err != nil {
		return err
	}
	return ensurePostgresDebugger(false)
}

// ensurePostgresDebugger sets up EDB's pldebugger, which phpPgAdmin's Debug
// button needs: the plugin installed on the server, plugin_debugger in
// shared_preload_libraries, and the pldbgapi extension in each database.
// When ask is true it first asks whether to set it up at all.
func ensurePostgresDebugger(ask bool) error {
	ui.Header("PL/pgSQL debugger")

	available := pldbgapiAvailable()
	preloaded, err := pluginDebuggerPreloaded()
	if err != nil {
		return err
	}

	if !available || !preloaded {
		if ask {
			ok, err := ui.Confirm("Install the PL/pgSQL debugger (pldebugger)? This restarts PostgreSQL.", true)
			if err != nil {
				return err
			}
			if !ok {
				ui.Info("Skipped — run `phnx setup pgdebugger` to add it later.")
				return nil
			}
		}
		if !available {
			if err := installPldebugger(); err != nil {
				return fmt.Errorf("installing pldebugger: %w", err)
			}
			if !pldbgapiAvailable() {
				return fmt.Errorf("pldebugger was installed but PostgreSQL does not list the pldbgapi extension")
			}
			ui.Success("pldebugger installed")
		}
		if !preloaded {
			if err := preloadPluginDebugger(); err != nil {
				return err
			}
		}
	}

	createPldbgapiEverywhere()
	ui.Success("PL/pgSQL debugger is ready")
	return nil
}

func pldbgapiAvailable() bool {
	out, err := runPostgresSuperuserSQL("SELECT 1 FROM pg_available_extensions WHERE name = 'pldbgapi';")
	return err == nil && strings.TrimSpace(out) == "1"
}

func pluginDebuggerPreloaded() (bool, error) {
	libs, err := sharedPreloadLibraries()
	if err != nil {
		return false, err
	}
	for _, l := range libs {
		if l == "plugin_debugger" {
			return true, nil
		}
	}
	return false, nil
}

func sharedPreloadLibraries() ([]string, error) {
	out, err := runPostgresSuperuserSQL("SHOW shared_preload_libraries;")
	if err != nil {
		return nil, fmt.Errorf("reading shared_preload_libraries: %w", err)
	}
	var libs []string
	for l := range strings.SplitSeq(strings.TrimSpace(out), ",") {
		if l = strings.Trim(strings.TrimSpace(l), `"`); l != "" {
			libs = append(libs, l)
		}
	}
	return libs, nil
}

// preloadPluginDebugger adds plugin_debugger to shared_preload_libraries
// (keeping what is already there) and restarts the server to load it.
func preloadPluginDebugger() error {
	libs, err := sharedPreloadLibraries()
	if err != nil {
		return err
	}
	libs = append(libs, "plugin_debugger")
	// Each library is its own literal: a single 'a,b' would be read as one name.
	quoted := make([]string, len(libs))
	for i, l := range libs {
		quoted[i] = database.QuotePgLiteral(l)
	}
	if _, err := runPostgresSuperuserSQL(fmt.Sprintf("ALTER SYSTEM SET shared_preload_libraries = %s;", strings.Join(quoted, ", "))); err != nil {
		return fmt.Errorf("setting shared_preload_libraries: %w", err)
	}

	service := detectPostgres()
	ui.Info(fmt.Sprintf("Restarting %s to load plugin_debugger...", service))
	if err := system.NewServiceManager().Restart(service); err != nil {
		return fmt.Errorf("restarting %s: %w", service, err)
	}
	if err := waitForPostgres(); err != nil {
		return err
	}
	ui.Success("Added plugin_debugger to shared_preload_libraries")
	return nil
}

func waitForPostgres() error {
	var err error
	for range 30 {
		if _, err = runPostgresSuperuserSQL("SELECT 1;"); err == nil {
			return nil
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("PostgreSQL did not come back after restarting: %w", err)
}

// createPldbgapiEverywhere creates the pldbgapi extension in every database
// that accepts connections, template1 included so new databases get it too.
func createPldbgapiEverywhere() {
	out, err := runPostgresSuperuserSQL("SELECT datname FROM pg_database WHERE datallowconn ORDER BY datname;")
	if err != nil {
		ui.Warn(fmt.Sprintf("Could not list databases: %v", err))
		return
	}
	for db := range strings.FieldsSeq(out) {
		if _, err := runPostgresSuperuserSQLIn(db, "CREATE EXTENSION IF NOT EXISTS pldbgapi;"); err != nil {
			ui.Warn(fmt.Sprintf("Could not create pldbgapi in %s: %v", db, err))
		}
	}
}

// installPldebugger installs the server plugin from the distro packages where
// they have it (Debian/Ubuntu), and otherwise builds it from source with PGXS.
func installPldebugger() error {
	if runtime.GOOS != "darwin" && system.IsDebian() {
		major, err := postgresMajorVersion()
		if err != nil {
			return err
		}
		ui.Info("Installing pldebugger...")
		return system.Run("apt-get", "install", "-y", fmt.Sprintf("postgresql-%d-pldebugger", major))
	}
	if runtime.GOOS != "darwin" && system.IsFedora() {
		// PGXS and the server headers come with the -devel package.
		if err := system.Run("dnf", "install", "-y", "postgresql-server-devel"); err != nil {
			return err
		}
	}
	return buildPldebuggerFromSource()
}

func postgresMajorVersion() (int, error) {
	out, err := runPostgresSuperuserSQL("SHOW server_version_num;")
	if err != nil {
		return 0, err
	}
	num, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, fmt.Errorf("parsing server_version_num %q: %w", out, err)
	}
	return num / 10000, nil
}

func buildPldebuggerFromSource() error {
	for _, tool := range []string{"make", "cc", "tar"} {
		if _, err := exec.LookPath(tool); err != nil {
			hint := "install a C toolchain"
			switch {
			case runtime.GOOS == "darwin":
				hint = "run `xcode-select --install`"
			case system.IsArch():
				hint = "run `sudo pacman -S --needed base-devel`"
			}
			return fmt.Errorf("building pldebugger needs %s — %s", tool, hint)
		}
	}

	pgConfig, err := serverPgConfig()
	if err != nil {
		return err
	}

	srcDir, err := os.MkdirTemp("", "phnx-pldebugger-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(srcDir)

	ui.Info("Downloading pldebugger source...")
	tarball, err := download(pldebuggerSourceURL)
	if err != nil {
		return err
	}
	tarPath := filepath.Join(srcDir, "pldebugger.tar.gz")
	if err := os.WriteFile(tarPath, tarball, 0644); err != nil {
		return err
	}
	if err := system.RunUser("tar", "-xzf", tarPath, "-C", srcDir, "--strip-components=1"); err != nil {
		return err
	}

	// with_llvm=no skips the JIT bitcode, which would also need clang.
	makeArgs := []string{"make", "-C", srcDir, "USE_PGXS=1", "PG_CONFIG=" + pgConfig, "with_llvm=no"}
	ui.Info("Building pldebugger...")
	if err := system.RunUser(makeArgs...); err != nil {
		return err
	}
	installArgs := append(makeArgs, "install")
	if runtime.GOOS == "darwin" {
		// The Homebrew prefix belongs to the user; no sudo.
		return system.RunUser(installArgs...)
	}
	return system.Run(installArgs...)
}

// serverPgConfig finds the pg_config belonging to the running server, so the
// plugin is built for the right version and installed where it looks.
func serverPgConfig() (string, error) {
	out, err := runPostgresSuperuserSQL("SELECT setting FROM pg_config WHERE name = 'BINDIR';")
	if err == nil {
		if p := filepath.Join(strings.TrimSpace(out), "pg_config"); strings.TrimSpace(out) != "" {
			if _, statErr := os.Stat(p); statErr == nil {
				return p, nil
			}
		}
	}
	if p, err := exec.LookPath("pg_config"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("pg_config not found — install the PostgreSQL server development package")
}
