package tui

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lylemi/ja3proxy/internal/ja3proxy/traffic"
)

type Config struct {
	ListenAddress string
	TLSClient     string
	TLSVersion    string
}

type ServeFunc func(context.Context) error

func Run(ctx context.Context, config Config, monitor *traffic.TrafficMonitor, serve ServeFunc) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if serve == nil {
		return fmt.Errorf("serve function is required")
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- serve(runCtx)
	}()

	program := tea.NewProgram(
		newTrafficTUIModel(config, monitor, cancel, errCh),
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
