package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
)

var (
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("212")).
			Padding(0, 1)

	cellStyle = lipgloss.NewStyle().
			Padding(0, 1)

	borderStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))
)

// PrintTable renders a lipgloss table to stdout.
func PrintTable(headers []string, rows [][]string) {
	t := table.New().
		Border(lipgloss.NormalBorder()).
		BorderStyle(borderStyle).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return headerStyle
			}
			return cellStyle
		}).
		Headers(headers...).
		Rows(rows...)

	fmt.Println(t.Render())
}

// StatusHealthy returns a styled "healthy" string.
func StatusHealthy() string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("46")).Render("healthy")
}

// StatusDegraded returns a styled "degraded" string with a reason.
func StatusDegraded(reason string) string {
	msg := "degraded"
	if reason != "" {
		msg += " (" + reason + ")"
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render(msg)
}

// Success prints a success message.
func Success(msg string) {
	fmt.Println(lipgloss.NewStyle().Foreground(lipgloss.Color("46")).Render("✓ " + msg))
}

// Info prints an info message.
func Info(msg string) {
	fmt.Println(lipgloss.NewStyle().Foreground(lipgloss.Color("33")).Render("→ " + msg))
}

// Warn prints a warning message.
func Warn(msg string) {
	fmt.Println(lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render("⚠ " + msg))
}

// Error prints an error message.
func Error(msg string) {
	fmt.Println(lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("✗ " + msg))
}

// Header prints a section header.
func Header(msg string) {
	fmt.Println(lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("212")).
		Render(msg))
}

// Separator prints a horizontal rule.
func Separator() {
	fmt.Println(lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(strings.Repeat("─", 60)))
}
