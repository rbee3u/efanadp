package tui

import (
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/lipgloss"
)

const (
	paneContentInset    = 2
	nodeSelectionGutter = 2
)

var (
	colorAccent       = lipgloss.AdaptiveColor{Light: "#0F766E", Dark: "#5EEAD4"}
	colorSelected     = lipgloss.AdaptiveColor{Light: "#7C3AED", Dark: "#C084FC"}
	colorSuccess      = lipgloss.AdaptiveColor{Light: "#15803D", Dark: "#4ADE80"}
	colorWarning      = lipgloss.AdaptiveColor{Light: "#B45309", Dark: "#FBBF24"}
	colorError        = lipgloss.AdaptiveColor{Light: "#B91C1C", Dark: "#F87171"}
	colorMuted        = lipgloss.AdaptiveColor{Light: "#6B7280", Dark: "#9CA3AF"}
	colorSubtle       = lipgloss.AdaptiveColor{Light: "#9CA3AF", Dark: "#6B7280"}
	colorText         = lipgloss.AdaptiveColor{Light: "#111827", Dark: "#E5E7EB"}
	colorDataUpload   = lipgloss.AdaptiveColor{Light: "#C2410C", Dark: "#FB923C"}
	colorDataDownload = lipgloss.AdaptiveColor{Light: "#0369A1", Dark: "#38BDF8"}

	valueStyle  = lipgloss.NewStyle().Foreground(colorText)
	mutedStyle  = lipgloss.NewStyle().Foreground(colorMuted)
	subtleStyle = lipgloss.NewStyle().Foreground(colorSubtle)

	helpStyle    = lipgloss.NewStyle().Foreground(colorMuted)
	helpKeyStyle = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)

	statusInfoStyle    = lipgloss.NewStyle().Foreground(colorMuted)
	statusSuccessStyle = lipgloss.NewStyle().Foreground(colorSuccess)
	statusWarningStyle = lipgloss.NewStyle().Foreground(colorWarning)
	statusErrorStyle   = lipgloss.NewStyle().Foreground(colorError)

	tableHeaderStyle = lipgloss.NewStyle().Bold(true).Foreground(colorMuted)
	tableRuleStyle   = lipgloss.NewStyle().Foreground(colorSubtle)
	trafficUpStyle   = lipgloss.NewStyle().Foreground(colorDataUpload)
	trafficDownStyle = lipgloss.NewStyle().Foreground(colorDataDownload)

	paginationActiveDotStyle   = lipgloss.NewStyle().Foreground(colorAccent)
	paginationInactiveDotStyle = lipgloss.NewStyle().Foreground(colorSubtle)

	connectionIDStyle     = lipgloss.NewStyle().Foreground(colorSubtle)
	connectionSourceStyle = lipgloss.NewStyle().Foreground(colorMuted)
	connectionTargetStyle = lipgloss.NewStyle().Foreground(colorText)
	connectionArrowStyle  = lipgloss.NewStyle().Foreground(colorSubtle)
	connectionRouteStyle  = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	connectionTimeStyle   = lipgloss.NewStyle().Foreground(colorMuted)

	appWindowStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorSubtle).
			Padding(0, 1)
	headerPaneStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(colorSubtle).
			Padding(0, paneContentInset)
	mainPaneStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(colorSubtle).
			Padding(0, paneContentInset)

	appBrandStyle   = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	nodeTitleStyle  = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	nodeDetailStyle = lipgloss.NewStyle().Foreground(colorMuted)
)

func styledNodeDelegate() list.DefaultDelegate {
	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = true
	delegate.SetHeight(2)
	delegate.SetSpacing(0)
	delegate.Styles.NormalTitle = nodeTitleStyle.
		Padding(0, 0, 0, nodeSelectionGutter)
	delegate.Styles.NormalDesc = nodeDetailStyle.
		Padding(0, 0, 0, nodeSelectionGutter)
	delegate.Styles.SelectedTitle = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(colorSelected).
		Bold(true).
		Foreground(colorSelected).
		Padding(0, 0, 0, nodeSelectionGutter-1)
	delegate.Styles.SelectedDesc = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(colorSelected).
		Foreground(colorMuted).
		Padding(0, 0, 0, nodeSelectionGutter-1)
	delegate.Styles.DimmedTitle = lipgloss.NewStyle().
		Foreground(colorSubtle).
		Padding(0, 0, 0, nodeSelectionGutter)
	delegate.Styles.DimmedDesc = lipgloss.NewStyle().
		Foreground(colorSubtle).
		Padding(0, 0, 0, nodeSelectionGutter)
	delegate.Styles.FilterMatch = lipgloss.NewStyle().
		Bold(true).
		Foreground(colorWarning)
	return delegate
}

func applyNodeListStyle(l *list.Model) {
	l.Styles.TitleBar = lipgloss.NewStyle().Padding(0, 0, 1, nodeSelectionGutter)
	l.Styles.Title = lipgloss.NewStyle().
		Bold(true).
		Foreground(colorAccent)
	l.Styles.FilterPrompt = helpKeyStyle
	l.Styles.FilterCursor = lipgloss.NewStyle().Foreground(colorAccent)
	l.Styles.DefaultFilterCharacterMatch = lipgloss.NewStyle().
		Bold(true).
		Foreground(colorWarning)
	l.Styles.StatusBar = lipgloss.NewStyle().
		Foreground(colorMuted).
		Padding(0, 0, 1, nodeSelectionGutter)
	l.Styles.StatusEmpty = subtleStyle
	l.Styles.StatusBarActiveFilter = valueStyle
	l.Styles.StatusBarFilterCount = subtleStyle
	l.Styles.NoItems = mutedStyle
	l.Styles.PaginationStyle = lipgloss.NewStyle().
		Foreground(colorMuted).
		PaddingLeft(nodeSelectionGutter)
	l.Styles.HelpStyle = helpStyle.PaddingTop(1)
	l.Styles.ActivePaginationDot = paginationActiveDotStyle.SetString("•")
	l.Styles.InactivePaginationDot = paginationInactiveDotStyle.SetString("•")
	l.Styles.DividerDot = lipgloss.NewStyle().
		Foreground(colorSubtle).
		SetString(" • ")
	l.FilterInput.PromptStyle = l.Styles.FilterPrompt
	l.FilterInput.TextStyle = valueStyle
	l.FilterInput.PlaceholderStyle = subtleStyle
	l.FilterInput.CompletionStyle = subtleStyle
	l.FilterInput.Cursor.Style = l.Styles.FilterCursor
	l.Paginator.ActiveDot = l.Styles.ActivePaginationDot.String()
	l.Paginator.InactiveDot = l.Styles.InactivePaginationDot.String()
}

func statusStyle(kind statusKind) lipgloss.Style {
	switch kind {
	case statusInfo:
		return statusInfoStyle
	case statusError:
		return statusErrorStyle
	case statusSuccess:
		return statusSuccessStyle
	case statusWarning:
		return statusWarningStyle
	default:
		return statusInfoStyle
	}
}
