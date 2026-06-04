package ui

import (
	"fmt"
	"os"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type spinnerModel struct {
	spinner  spinner.Model
	message  string
	quitting bool
}

func (m spinnerModel) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m spinnerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m spinnerModel) View() string {
	return fmt.Sprintf("%s %s", m.spinner.View(), m.message)
}

// RunWithSpinner runs fn while displaying a spinner with message.
// On error, prints the error and exits.
func RunWithSpinner(message string, fn func() error) error {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	done := make(chan error, 1)
	go func() {
		done <- fn()
	}()

	// Simple non-TUI spinner for CI / non-TTY environments
	if !isTerminal() {
		fmt.Printf("  %s...\n", message)
		err := <-done
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
		}
		return err
	}

	m := spinnerModel{spinner: s, message: message}
	p := tea.NewProgram(m, tea.WithOutput(os.Stderr))

	go func() {
		err := <-done
		p.Quit()
		if err != nil {
			fmt.Fprintf(os.Stderr, "\nerror: %v\n", err)
		}
	}()

	if _, err := p.Run(); err != nil {
		return err
	}
	return nil
}

func isTerminal() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
