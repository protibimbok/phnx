package site

import (
	"fmt"

	"github.com/protibimbok/phnx/internal/ssl"
	"github.com/protibimbok/phnx/internal/ui"
)

func EnsureCertificate(subdomain, domain string) error {
	if err := EnsureCA(); err != nil {
		return err
	}
	if err := ssl.IssueCert(subdomain, domain); err != nil {
		return fmt.Errorf("issuing certificate: %w", err)
	}
	ui.Success(fmt.Sprintf("Certificate issued for %s (and *.%s)", domain, domain))
	return nil
}

func EnsureCA() error {
	created, err := ssl.EnsureCA()
	if err != nil {
		return fmt.Errorf("creating local CA: %w", err)
	}
	if created {
		ui.Success(fmt.Sprintf("Created local CA: %s", ssl.CACertPath()))
	}

	if ssl.IsTrusted() {
		return nil
	}

	ui.Info("Installing the phnx CA into the system trust store (needs sudo)…")
	if err := ssl.InstallTrust(); err != nil {
		return fmt.Errorf("installing CA into trust store: %w", err)
	}
	ui.Success("CA trusted by the system")

	updated, nssErr := ssl.InstallNSSTrust()
	if nssErr != nil {
		ui.Warn(nssErr.Error())
	}
	if len(updated) > 0 {
		ui.Success(fmt.Sprintf("CA added to %d browser certificate database(s)", len(updated)))
		ui.Info("Restart Firefox/Chrome if they are open so they pick up the new CA.")
	}

	if ssl.IsWSL() {
		ok, _ := ui.Confirm("Running under WSL — also trust the CA in Windows (for Windows browsers)?", true)
		if ok {
			if err := ssl.InstallWindowsTrust(); err != nil {
				ui.Warn(err.Error())
			} else {
				ui.Success("CA added to the Windows user Trusted Root store")
			}
		}
	}
	return nil
}
