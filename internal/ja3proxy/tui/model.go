package tui

import (
	"context"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lylemi/ja3proxy/internal/ja3proxy/traffic"
)

const trafficTUITickInterval = 500 * time.Millisecond

type trafficTickMsg time.Time

type trafficServerStoppedMsg struct {
	err error
}

type trafficTUIModel struct {
	config    Config
	monitor   *traffic.TrafficMonitor
	cancel    context.CancelFunc
	serverErr error
	errCh     <-chan error
	table     table.Model
	snapshot  traffic.TrafficSnapshot
	previous  traffic.TrafficSnapshot
	width     int
	height    int
	paused    bool
}

func newTrafficTUIModel(
	config Config,
	monitor *traffic.TrafficMonitor,
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

func (model *trafficTUIModel) refresh() {
	model.previous = model.snapshot
	if model.monitor == nil {
		now := time.Now()
		model.snapshot = traffic.TrafficSnapshot{
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
