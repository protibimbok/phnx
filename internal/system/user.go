package system

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strings"
)

// RealUser returns the non-root user who invoked phnx (even under sudo).
func RealUser() (*user.User, error) {
	// When running under sudo, SUDO_USER holds the invoking user.
	if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" {
		u, err := user.Lookup(sudoUser)
		if err != nil {
			return nil, fmt.Errorf("looking up SUDO_USER %q: %w", sudoUser, err)
		}
		if u.Uid == "0" {
			return nil, fmt.Errorf("resolved user is root — please run as a regular user")
		}
		return u, nil
	}

	u, err := user.Current()
	if err != nil {
		return nil, fmt.Errorf("getting current user: %w", err)
	}
	if u.Uid == "0" {
		// Try logname as last resort
		if name, err2 := logname(); err2 == nil && name != "root" {
			u2, err3 := user.Lookup(name)
			if err3 == nil {
				return u2, nil
			}
		}
		return nil, fmt.Errorf("running as root is not allowed — use sudo from a regular user account")
	}
	return u, nil
}

func logname() (string, error) {
	out, err := exec.Command("logname").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// PrimaryGroup returns the primary group name for a user.
func PrimaryGroup(u *user.User) (string, error) {
	g, err := user.LookupGroupId(u.Gid)
	if err != nil {
		return "", fmt.Errorf("looking up group %s: %w", u.Gid, err)
	}
	return g.Name, nil
}
