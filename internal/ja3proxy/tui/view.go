package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
	"github.com/lylemi/ja3proxy/internal/ja3proxy/traffic"
)

func (model trafficTUIModel) View() string {
	if model.width == 0 {
		return "Starting JA3Proxy traffic dashboard..."
	}

	width := model.width
	if width < 40 {
		width = 40
	}
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86")).
		Render("JA3Proxy Traffic")
	status := model.statusLine()
	if model.paused {
		status += " | paused"
	}

	events := model.eventsView()
	help := lipgloss.NewStyle().
		Foreground(lipgloss.Color("244")).
		Render("q quit | p pause | r clear closed | up/down select")

	return lipgloss.NewStyle().
		Width(width).
		Padding(0, 1).
		Render(lipgloss.JoinVertical(
			lipgloss.Left,
			title,
			status,
			"",
			model.table.View(),
			"",
			events,
			help,
		))
}

func (model trafficTUIModel) statusLine() string {
	fingerprint := ""
	fingerprint = model.config.TLSClient + "@" + model.config.TLSVersion
	if fingerprint == "@" {
		fingerprint = "unknown"
	}

	return fmt.Sprintf(
		"listen %s | uptime %s | active %d | total %d | up %s | down %s | tls %s",
		model.config.ListenAddress,
		formatDuration(model.snapshot.CapturedAt.Sub(model.snapshot.StartedAt)),
		model.snapshot.ActiveSessions,
		model.snapshot.TotalSessions,
		formatBytes(model.snapshot.TotalUploadBytes),
		formatBytes(model.snapshot.TotalDownloadBytes),
		fingerprint,
	)
}

func (model trafficTUIModel) eventsView() string {
	events := model.snapshot.Events
	if len(events) == 0 {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Render("events: no events")
	}

	limit := 4
	if model.height > 0 && model.height < 18 {
		limit = 2
	}
	if len(events) > limit {
		events = events[len(events)-limit:]
	}

	lines := []string{"events:"}
	for _, event := range events {
		target := event.Target
		if target == "" {
			target = "-"
		}
		line := fmt.Sprintf(
			"%s %-5s %-12s %s",
			event.Time.Format("15:04:05"),
			event.Level,
			target,
			event.Message,
		)
		if event.Error != "" {
			line += ": " + event.Error
		}
		lines = append(lines, truncateRunes(line, model.eventWidth()))
	}

	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("252")).
		Render(strings.Join(lines, "\n"))
}

func (model trafficTUIModel) eventWidth() int {
	if model.width <= 4 {
		return 80
	}
	return model.width - 4
}

func trafficTableColumns(width int) []table.Column {
	targetWidth := width - 72
	if targetWidth < 18 {
		targetWidth = 18
	}
	if targetWidth > 48 {
		targetWidth = 48
	}
	return []table.Column{
		{Title: "PROTO", Width: 12},
		{Title: "STATE", Width: 8},
		{Title: "TARGET", Width: targetWidth},
		{Title: "UP", Width: 10},
		{Title: "DOWN", Width: 10},
		{Title: "RATE", Width: 12},
		{Title: "AGE", Width: 8},
	}
}

func (model trafficTUIModel) trafficTableRows() []table.Row {
	if len(model.snapshot.Sessions) == 0 {
		return []table.Row{{
			"-",
			"-",
			"waiting for traffic",
			"-",
			"-",
			"-",
			"-",
		}}
	}

	rows := make([]table.Row, 0, len(model.snapshot.Sessions))
	for _, session := range model.snapshot.Sessions {
		ageEnd := model.snapshot.CapturedAt
		if !session.ClosedAt.IsZero() {
			ageEnd = session.ClosedAt
		}
		age := ageEnd.Sub(session.StartedAt)
		rows = append(rows, table.Row{
			truncateRunes(session.Protocol, 12),
			session.State,
			truncateRunes(session.Target, 48),
			formatBytes(session.UploadBytes),
			formatBytes(session.DownloadBytes),
			model.formatSessionRate(session, age),
			formatDuration(age),
		})
	}
	return rows
}

func (model trafficTUIModel) formatSessionRate(session traffic.TrafficSessionSnapshot, age time.Duration) string {
	if session.State != traffic.StateActive || model.previous.CapturedAt.IsZero() {
		return formatRate(session.UploadBytes+session.DownloadBytes, age)
	}

	previous, ok := findPreviousTrafficSession(model.previous, session.ID)
	if !ok {
		return formatRate(session.UploadBytes+session.DownloadBytes, age)
	}
	deltaBytes := session.UploadBytes + session.DownloadBytes - previous.UploadBytes - previous.DownloadBytes
	if deltaBytes < 0 {
		deltaBytes = 0
	}
	return formatRate(deltaBytes, model.snapshot.CapturedAt.Sub(model.previous.CapturedAt))
}

func findPreviousTrafficSession(snapshot traffic.TrafficSnapshot, id uint64) (traffic.TrafficSessionSnapshot, bool) {
	for _, session := range snapshot.Sessions {
		if session.ID == id {
			return session, true
		}
	}
	return traffic.TrafficSessionSnapshot{}, false
}
