package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/huh"
)

// AskText shows a simple text input prompt.
func AskText(title, placeholder, defaultVal string) (string, error) {
	var result string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title(title).
				Placeholder(placeholder).
				Value(&result),
		),
	)
	if err := form.Run(); err != nil {
		return "", err
	}
	if strings.TrimSpace(result) == "" {
		return defaultVal, nil
	}
	return result, nil
}

// AskSelect shows a single-choice selection prompt.
func AskSelect(title string, options []string) (string, error) {
	if len(options) == 0 {
		return "", fmt.Errorf("no options provided")
	}
	var result string
	opts := make([]huh.Option[string], len(options))
	for i, o := range options {
		opts[i] = huh.NewOption(o, o)
	}
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title(title).
				Options(opts...).
				Value(&result),
		),
	)
	if err := form.Run(); err != nil {
		return "", err
	}
	return result, nil
}

// Confirm shows a yes/no confirmation prompt.
func Confirm(title string, defaultYes bool) (bool, error) {
	var result bool
	result = defaultYes
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(title).
				Value(&result),
		),
	)
	if err := form.Run(); err != nil {
		return false, err
	}
	return result, nil
}

// AskPassword shows a masked text input for passwords.
func AskPassword(title string) (string, error) {
	var result string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title(title).
				EchoMode(huh.EchoModePassword).
				Value(&result),
		),
	)
	if err := form.Run(); err != nil {
		return "", err
	}
	return result, nil
}
