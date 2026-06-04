package system

import (
	"bufio"
	"os"
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

// NginxWebUser returns the user/group that nginx uses for socket ownership.
// On macOS, Homebrew nginx runs as the invoking user — pass realUser for that case.
// On Linux it's a fixed system account.
func NginxWebUser() string {
	if IsArch() {
		return "http"
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
