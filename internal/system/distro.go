package system

import (
	"bufio"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

type Distro int

const (
	DistroUnknown Distro = iota
	DistroDebian
	DistroUbuntu
	DistroArch
	DistroFedora
	DistroRHEL
)

var detectedDistro Distro = -1

// GetDistro reads /etc/os-release and returns the Linux distribution.
func GetDistro() Distro {
	if detectedDistro != -1 {
		return detectedDistro
	}
	f, err := os.Open("/etc/os-release")
	if err != nil {
		detectedDistro = DistroUnknown
		return detectedDistro
	}
	defer f.Close()

	values := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			values[parts[0]] = strings.Trim(parts[1], `"`)
		}
	}

	id := strings.ToLower(values["ID"])
	idLike := strings.ToLower(values["ID_LIKE"])

	switch {
	case id == "arch" || strings.Contains(idLike, "arch"):
		detectedDistro = DistroArch
	case id == "ubuntu" || strings.Contains(idLike, "ubuntu"):
		detectedDistro = DistroUbuntu
	case id == "debian" || strings.Contains(idLike, "debian"):
		detectedDistro = DistroDebian
	case id == "fedora" || strings.Contains(idLike, "fedora"):
		detectedDistro = DistroFedora
	case id == "rhel" || id == "centos" || strings.Contains(idLike, "rhel"):
		detectedDistro = DistroRHEL
	default:
		detectedDistro = DistroUnknown
	}
	return detectedDistro
}

func IsArch() bool    { return GetDistro() == DistroArch }
func IsDebian() bool  { d := GetDistro(); return d == DistroDebian || d == DistroUbuntu }
func IsFedora() bool  { d := GetDistro(); return d == DistroFedora || d == DistroRHEL }

// OSReleaseValue returns a raw value from /etc/os-release (e.g. "UBUNTU_CODENAME"),
// or "" if the file or key is missing.
func OSReleaseValue(key string) string {
	f, err := os.Open("/etc/os-release")
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		parts := strings.SplitN(scanner.Text(), "=", 2)
		if len(parts) == 2 && parts[0] == key {
			return strings.Trim(parts[1], `"`)
		}
	}
	return ""
}

// UbuntuCodename returns the Ubuntu base suite (e.g. "noble") for the running
// system. On derivatives like Linux Mint the system's own VERSION_CODENAME is
// not a valid Ubuntu suite, so UBUNTU_CODENAME is preferred; it falls back to
// VERSION_CODENAME and finally `lsb_release -cs`.
func UbuntuCodename() string {
	if c := OSReleaseValue("UBUNTU_CODENAME"); c != "" {
		return c
	}
	if c := OSReleaseValue("VERSION_CODENAME"); c != "" {
		return c
	}
	out, err := exec.Command("lsb_release", "-cs").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// NginxWebUser returns the user/group that nginx uses for socket ownership.
// On macOS, Homebrew nginx runs as the invoking user — pass realUser for that case.
// On Linux it's a fixed system account.
func NginxWebUser() string {
	if IsArch() {
		return "http"
	}
	if IsFedora() {
		// The Fedora/RHEL nginx RPM runs as user/group "nginx"; there is no
		// www-data account on these distros.
		return "nginx"
	}
	return "www-data"
}

// NginxWebUserFor returns the correct listen.owner for FPM pool configs.
// On macOS nginx runs as the real user, not a system account.
func NginxWebUserFor(realUser string) string {
	switch runtime.GOOS {
	case "darwin":
		return realUser
	default:
		return NginxWebUser()
	}
}

// ArchVersionTag converts "8.2" → "82" for Arch AUR package naming.
func ArchVersionTag(version string) string {
	return strings.ReplaceAll(version, ".", "")
}
