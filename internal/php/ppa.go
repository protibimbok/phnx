package php

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/protibimbok/phnx/internal/system"
	"github.com/protibimbok/phnx/internal/ui"
)

// EnsurePPA ensures the PHP package source is configured for this OS/distro.
func EnsurePPA() error {
	if runtime.GOOS == "darwin" {
		return ensureMacTap()
	}
	switch {
	case system.IsArch():
		return ensureArchlinuxCN()
	case system.IsDebian():
		return ensureOndrejPPA()
	case system.IsFedora():
		return ensureRemiRepo()
	default:
		return nil
	}
}

// ondrej/php PPA signing keys. The first is the Launchpad-generated PPA key,
// the second is Ondřej Surý's personal key; releases may be signed by either,
// so both must be present or `apt-get update` fails with NO_PUBKEY.
var ondrejKeyFingerprints = []string{
	"B8DC7E53946656EFBCE4C1DD71DAEAAB4AD4CAB6",
	"14AA40EC0831756756D7F66C4F4EA0AAE5267A6C",
}

const ondrejKeyring = "/usr/share/keyrings/ondrej-php.gpg"

func ensureOndrejPPA() error {
	// Check if PPA already provides packages
	out, _ := exec.Command("apt-cache", "policy", fmt.Sprintf("php%s-fpm", LatestStable())).Output()
	if strings.Contains(string(out), "ondrej") || strings.Contains(string(out), "ppa") {
		return nil
	}
	// Also check sources list files directly
	grepOut, _ := exec.Command("grep", "-r", "ondrej/php", "/etc/apt/sources.list.d/").Output()
	if len(grepOut) > 0 {
		return nil
	}

	fmt.Println("Adding ondrej/php PPA...")

	// Set the repo up manually with an explicit keyring. On Linux Mint and other
	// Ubuntu derivatives, `add-apt-repository` does not reliably import the PPA's
	// signing keys (leading to NO_PUBKEY errors), and may use the derivative's own
	// codename instead of the Ubuntu base suite. Deriving the suite from
	// UBUNTU_CODENAME and importing keys ourselves avoids both problems.
	if codename := system.UbuntuCodename(); codename != "" {
		if err := setupOndrejRepo(codename); err != nil {
			return err
		}
		return system.Run("apt-get", "update")
	}

	// Fallback: let add-apt-repository figure out the suite, then make sure the
	// keys are present even if it failed to import them.
	if err := system.Run("add-apt-repository", "-y", "ppa:ondrej/php"); err != nil {
		return fmt.Errorf("adding ondrej/php PPA: %w", err)
	}
	if err := importOndrejKeys(); err != nil {
		return err
	}
	return system.Run("apt-get", "update")
}

// setupOndrejRepo imports the signing keys and writes a signed-by sources entry
// for the given Ubuntu suite.
func setupOndrejRepo(codename string) error {
	if err := importOndrejKeys(); err != nil {
		return err
	}
	entry := fmt.Sprintf(
		"deb [signed-by=%s] https://ppa.launchpadcontent.net/ondrej/php/ubuntu %s main\n",
		ondrejKeyring, codename,
	)
	if err := system.WriteFile("/etc/apt/sources.list.d/ondrej-php.list", entry); err != nil {
		return fmt.Errorf("writing ondrej/php sources list: %w", err)
	}
	return nil
}

// importOndrejKeys fetches the PPA signing keys from the Ubuntu keyserver into a
// dedicated keyring file referenced via signed-by.
func importOndrejKeys() error {
	// gpg is required to receive and store the keys; install it if missing.
	if _, err := exec.LookPath("gpg"); err != nil {
		if err := system.Run("apt-get", "install", "-y", "gnupg"); err != nil {
			return fmt.Errorf("installing gnupg for key import: %w", err)
		}
	}
	args := []string{
		"gpg", "--batch", "--no-default-keyring", "--keyring", ondrejKeyring,
		"--keyserver", "hkps://keyserver.ubuntu.com", "--recv-keys",
	}
	args = append(args, ondrejKeyFingerprints...)
	if err := system.Run(args...); err != nil {
		return fmt.Errorf("importing ondrej/php signing keys: %w", err)
	}
	return nil
}

func ensureRemiRepo() error {
	out, _ := exec.Command("dnf", "repolist").Output()
	if strings.Contains(string(out), "remi") {
		return nil
	}
	fmt.Println("Adding Remi PHP repository...")

	versionID := system.OSReleaseValue("VERSION_ID")
	if versionID == "" {
		return fmt.Errorf("could not determine OS version from /etc/os-release")
	}

	// Fedora has its own Remi stream keyed by the Fedora release number (e.g. 40,
	// 41). The Enterprise Linux stream (remi-release-9.rpm) must NOT be used on
	// Fedora — it depends on redhat-release/epel-release/system-release, none of
	// which exist there.
	if system.GetDistro() == system.DistroFedora {
		url := fmt.Sprintf("https://rpms.remirepo.net/fedora/remi-release-%s.rpm", versionID)
		return system.Run("dnf", "install", "-y", url)
	}

	// RHEL / CentOS Stream / derivatives use the Enterprise Linux stream, keyed by
	// the OS major version. remi-release depends on EPEL, so ensure that first.
	major, _, _ := strings.Cut(versionID, ".")
	if err := system.Run("dnf", "install", "-y",
		fmt.Sprintf("https://dl.fedoraproject.org/pub/epel/epel-release-latest-%s.noarch.rpm", major)); err != nil {
		return fmt.Errorf("installing EPEL (required by Remi): %w", err)
	}
	url := fmt.Sprintf("https://rpms.remirepo.net/enterprise/remi-release-%s.rpm", major)
	return system.Run("dnf", "install", "-y", url)
}

// archlinuxCNAvailable reports whether /etc/pacman.conf already has [archlinuxcn].
func archlinuxCNAvailable() bool {
	out, err := os.ReadFile("/etc/pacman.conf")
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "[archlinuxcn]")
}

// ensureArchlinuxCN adds the archlinuxcn repository to pacman.conf and imports
// its keyring so that pre-built PHP packages can be installed without compiling.
func ensureArchlinuxCN() error {
	if archlinuxCNAvailable() {
		return nil
	}

	fmt.Println("Adding archlinuxcn repository (pre-built PHP packages)...")

	// Append the repo block to pacman.conf
	addition := "\n[archlinuxcn]\nServer = https://repo.archlinuxcn.org/$arch\n"
	current, err := os.ReadFile("/etc/pacman.conf")
	if err != nil {
		return fmt.Errorf("reading /etc/pacman.conf: %w", err)
	}
	if err := system.WriteFile("/etc/pacman.conf", string(current)+addition); err != nil {
		return fmt.Errorf("updating /etc/pacman.conf: %w", err)
	}

	// Sync and install the keyring
	if err := system.Run("pacman", "-Sy", "--noconfirm", "archlinuxcn-keyring"); err != nil {
		return fmt.Errorf("installing archlinuxcn-keyring: %w", err)
	}
	return nil
}

func ensureMacTap() error {
	out, _ := exec.Command("brew", "tap").Output()
	if strings.Contains(string(out), "shivammathur/php") {
		return nil
	}
	fmt.Println("Tapping shivammathur/php...")
	// brew must NOT run under sudo
	return system.RunUser("brew", "tap", "shivammathur/php")
}

// Install installs the given PHP version and common extensions.
func Install(version string) error {
	if runtime.GOOS == "darwin" {
		return installMac(version)
	}
	switch {
	case system.IsArch():
		return installArch(version)
	case system.IsDebian():
		return installDebian(version)
	case system.IsFedora():
		return installFedora(version)
	default:
		return fmt.Errorf("unsupported distro for automated PHP install — please install php%s-fpm manually", version)
	}
}

func installDebian(version string) error {
	pkgs := []string{
		fmt.Sprintf("php%s-fpm", version),
		fmt.Sprintf("php%s-cli", version),
		fmt.Sprintf("php%s-mysql", version),
		fmt.Sprintf("php%s-curl", version),
		fmt.Sprintf("php%s-gd", version),
		fmt.Sprintf("php%s-mbstring", version),
		fmt.Sprintf("php%s-xml", version),
		fmt.Sprintf("php%s-zip", version),
		fmt.Sprintf("php%s-bcmath", version),
		fmt.Sprintf("php%s-intl", version),
	}
	return system.Run(append([]string{"apt-get", "install", "-y"}, pkgs...)...)
}

func installArch(version string) error {
	tag := system.ArchVersionTag(version)
	pkg := fmt.Sprintf("php%s", tag)

	// Prefer pacman if the package is actually available in a synced repo
	if pacmanHasPackage(pkg) {
		fmt.Printf("Installing %s from repos (pre-built)...\n", pkg)
		return system.Run("pacman", "-S", "--noconfirm", pkg)
	}

	// Not in repos yet — offer to add archlinuxcn for pre-built packages
	if !archlinuxCNAvailable() {
		setup, err := ui.Confirm("archlinuxcn repo not found. Set it up for pre-built PHP packages?", true)
		if err != nil {
			return err
		}
		if setup {
			if err := ensureArchlinuxCN(); err != nil {
				return err
			}
			if pacmanHasPackage(pkg) {
				fmt.Printf("Installing %s from archlinuxcn (pre-built)...\n", pkg)
				return system.Run("pacman", "-S", "--noconfirm", pkg)
			}
		}
	}

	// Fall back to AUR helper — run interactively so the user can answer
	// prompts (clean build, diffs, etc.) themselves.
	aurHelper := detectAURHelper()
	if aurHelper != "" {
		fmt.Printf("%s not found in pacman repos — installing via %s...\n", pkg, aurHelper)
		return system.RunUserTTY(aurHelper, "-S", pkg)
	}

	return fmt.Errorf(
		"cannot install PHP %s: package not in pacman repos and no AUR helper found\n"+
			"  Option 1 (recommended): set up archlinuxcn — https://www.archlinuxcn.org/archlinux-cn-repo-and-mirror/\n"+
			"  Option 2: install an AUR helper (yay or paru), then re-run",
		version,
	)
}

// pacmanHasPackage reports whether pkg is available in any synced pacman repository.
func pacmanHasPackage(pkg string) bool {
	err := exec.Command("pacman", "-Si", pkg).Run()
	return err == nil
}

func installFedora(version string) error {
	tag := system.ArchVersionTag(version) // "82" style
	return system.Run("dnf", "install", "-y",
		fmt.Sprintf("php%s-php-fpm", tag),
		fmt.Sprintf("php%s-php-cli", tag),
		fmt.Sprintf("php%s-php-mysqlnd", tag),
		fmt.Sprintf("php%s-php-gd", tag),
		fmt.Sprintf("php%s-php-mbstring", tag),
		fmt.Sprintf("php%s-php-xml", tag),
		fmt.Sprintf("php%s-php-bcmath", tag),
	)
}

func installMac(version string) error {
	pkg := fmt.Sprintf("shivammathur/php/php@%s", version)
	// brew must NOT run under sudo
	return system.RunUser("brew", "install", pkg)
}

// Uninstall removes the given PHP version.
func Uninstall(version string) error {
	if runtime.GOOS == "darwin" {
		return system.RunUser("brew", "uninstall", fmt.Sprintf("php@%s", version))
	}
	switch {
	case system.IsArch():
		tag := system.ArchVersionTag(version)
		pkg := fmt.Sprintf("php%s", tag)
		// pacman handles both archlinuxcn and AUR-installed packages for removal
		if err := system.Run("pacman", "-Rns", "--noconfirm", pkg); err != nil {
			// pacman -Rns fails for foreign (AUR) packages — fall back to AUR helper
			aurHelper := detectAURHelper()
			if aurHelper != "" {
				return system.RunUser(aurRemoveArgs(aurHelper, pkg)...)
			}
			return err
		}
		return nil
	case system.IsDebian():
		pkgs := []string{
			fmt.Sprintf("php%s-fpm", version),
			fmt.Sprintf("php%s-cli", version),
			fmt.Sprintf("php%s-mysql", version),
			fmt.Sprintf("php%s-curl", version),
			fmt.Sprintf("php%s-gd", version),
			fmt.Sprintf("php%s-mbstring", version),
			fmt.Sprintf("php%s-xml", version),
			fmt.Sprintf("php%s-zip", version),
			fmt.Sprintf("php%s-bcmath", version),
			fmt.Sprintf("php%s-intl", version),
		}
		return system.Run(append([]string{"apt-get", "remove", "-y"}, pkgs...)...)
	default:
		return fmt.Errorf("unsupported distro for automated PHP uninstall")
	}
}

// detectAURHelper returns the first available AUR helper binary.
func detectAURHelper() string {
	for _, h := range []string{"yay", "paru", "trizen", "aura"} {
		if _, err := exec.LookPath(h); err == nil {
			return h
		}
	}
	return ""
}

// aurRemoveArgs returns the full argument list for a non-interactive AUR removal.
func aurRemoveArgs(helper, pkg string) []string {
	return []string{helper, "-Rns", "--noconfirm", pkg}
}
