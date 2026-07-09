package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const trafficTUITickInterval = 500 * time.Millisecond

type trafficTickMsg time.Time

type trafficServerStoppedMsg struct {
	err error
}

type trafficTUIModel struct {
	config    *RunningConfig
	monitor   *TrafficMonitor
	cancel    context.CancelFunc
	serverErr error
	errCh     <-chan error
	table     table.Model
	snapshot  TrafficSnapshot
	previous  TrafficSnapshot
	width     int
	height    int
	paused    bool
}

func (app *App) serveWithTUI(ctx context.Context, proxy *Proxy) error {
	ctx = runtimeContext(ctx)
	if app.TrafficMonitor == nil {
		app.TrafficMonitor = NewTrafficMonitor()
	}
	if proxy != nil {
		proxy.WithTrafficMonitor(app.TrafficMonitor)
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- app.serve(runCtx, proxy)
	}()

	program := tea.NewProgram(
		newTrafficTUIModel(app.Config, app.TrafficMonitor, cancel, errCh),
		tea.WithAltScreen(),
	)
	returnedModel, tuiErr := program.Run()
	cancel()

	var serverErr error
	if model, ok := returnedModel.(trafficTUIModel); ok {
		serverErr = model.serverErr
	}
	if serverErr == nil {
		select {
		case serverErr = <-errCh:
		case <-time.After(5 * time.Second):
		}
	}
	if tuiErr != nil {
		return tuiErr
	}
	if serverErr != nil && !isExpectedTUIServerStop(serverErr) {
		return serverErr
	}
	return nil
}

func isExpectedTUIServerStop(err error) bool {
	return errors.Is(err, context.Canceled) ||
		errors.Is(err, http.ErrServerClosed) ||
		errors.Is(err, net.ErrClosed)
}

func newTrafficTUIModel(
	config *RunningConfig,
	monitor *TrafficMonitor,
	cancel context.CancelFunc,
	errCh <-chan error,
) trafficTUIModel {
	model := trafficTUIModel{
		config:  config,
		monitor: monitor,
		cancel:  cancel,
		errCh:   errCh,
		table: table.New(
			table.WithFocused(true),
			table.WithColumns(trafficTableColumns(100)),
			table.WithHeight(12),
		),
	}
	styles := table.DefaultStyles()
	styles.Header = styles.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(false)
	styles.Selected = styles.Selected.
		Foreground(lipgloss.Color("230")).
		Background(lipgloss.Color("57")).
		Bold(false)
	model.table.SetStyles(styles)
	model.refresh()
	return model
}

func (model trafficTUIModel) Init() tea.Cmd {
	return tea.Batch(trafficTick(), waitForTrafficServer(model.errCh))
}

func (model trafficTUIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		model.width = msg.Width
		model.height = msg.Height
		model.resizeTable()
		model.refresh()
		return model, nil
	case trafficTickMsg:
		if !model.paused {
			model.refresh()
		}
		return model, trafficTick()
	case trafficServerStoppedMsg:
		model.serverErr = msg.err
		return model, tea.Quit
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			if model.cancel != nil {
				model.cancel()
			}
			return model, tea.Quit
		case "p":
			model.paused = !model.paused
			return model, nil
		case "r":
			model.monitor.ResetClosed()
			model.refresh()
			return model, nil
		}
	}

	var cmd tea.Cmd
	model.table, cmd = model.table.Update(msg)
	return model, cmd
}

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

func (model *trafficTUIModel) refresh() {
	model.previous = model.snapshot
	if model.monitor == nil {
		now := time.Now()
		model.snapshot = TrafficSnapshot{
			CapturedAt: now,
			StartedAt:  now,
		}
	} else {
		model.snapshot = model.monitor.Snapshot()
	}
	model.table.SetRows(model.trafficTableRows())
}

func (model *trafficTUIModel) resizeTable() {
	if model.width <= 0 {
		return
	}
	tableWidth := model.width - 4
	if tableWidth < 40 {
		tableWidth = 40
	}
	tableHeight := model.height - 11
	if tableHeight < 4 {
		tableHeight = 4
	}
	model.table.SetWidth(tableWidth)
	model.table.SetHeight(tableHeight)
	model.table.SetColumns(trafficTableColumns(tableWidth))
}

func (model trafficTUIModel) statusLine() string {
	fingerprint := ""
	if model.config != nil {
		fingerprint = model.config.TLSClient + "@" + model.config.TLSVersion
	}
	if fingerprint == "@" {
		fingerprint = "unknown"
	}

	return fmt.Sprintf(
		"listen %s | uptime %s | active %d | total %d | up %s | down %s | tls %s",
		listenAddress(model.config),
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

func (model trafficTUIModel) formatSessionRate(session TrafficSessionSnapshot, age time.Duration) string {
	if session.State != trafficStateActive || model.previous.CapturedAt.IsZero() {
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

func findPreviousTrafficSession(snapshot TrafficSnapshot, id uint64) (TrafficSessionSnapshot, bool) {
	for _, session := range snapshot.Sessions {
		if session.ID == id {
			return session, true
		}
	}
	return TrafficSessionSnapshot{}, false
}

func trafficTick() tea.Cmd {
	return tea.Tick(trafficTUITickInterval, func(t time.Time) tea.Msg {
		return trafficTickMsg(t)
	})
}

func waitForTrafficServer(errCh <-chan error) tea.Cmd {
	return func() tea.Msg {
		err := <-errCh
		return trafficServerStoppedMsg{err: err}
	}
}

func listenAddress(config *RunningConfig) string {
	if config == nil {
		return defaultListen
	}
	return config.listenAddress()
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	value := float64(bytes)
	for _, suffix := range []string{"KiB", "MiB", "GiB", "TiB"} {
		value /= unit
		if value < unit {
			return fmt.Sprintf("%.1f %s", value, suffix)
		}
	}
	return fmt.Sprintf("%.1f PiB", value/unit)
}

func formatRate(bytes int64, duration time.Duration) string {
	if duration <= 0 {
		return "0 B/s"
	}
	return formatBytes(int64(float64(bytes)/duration.Seconds())) + "/s"
}

func formatDuration(duration time.Duration) string {
	if duration < 0 {
		duration = 0
	}
	if duration < time.Minute {
		return fmt.Sprintf("%ds", int(duration.Seconds()))
	}
	if duration < time.Hour {
		return fmt.Sprintf("%dm%02ds", int(duration.Minutes()), int(duration.Seconds())%60)
	}
	return fmt.Sprintf("%dh%02dm", int(duration.Hours()), int(duration.Minutes())%60)
}

func truncateRunes(value string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	if width <= 1 {
		return string(runes[:width])
	}
	return string(runes[:width-1]) + "~"
}
