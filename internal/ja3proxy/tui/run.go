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
	ctx = tuiContext(ctx)
	if serve == nil {
		return fmt.Errorf("serve function is required")
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := startServer(runCtx, serve)
	returnedModel, tuiErr := runProgram(config, monitor, cancel, errCh)
	cancel()

	return finishRun(returnedModel, tuiErr, errCh)
}

func tuiContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func startServer(ctx context.Context, serve ServeFunc) <-chan error {
	errCh := make(chan error, 1)
	go func() {
		errCh <- serve(ctx)
	}()
	return errCh
}

func runProgram(
	config Config,
	monitor *traffic.TrafficMonitor,
	cancel context.CancelFunc,
	errCh <-chan error,
) (tea.Model, error) {
	program := tea.NewProgram(
		newTrafficTUIModel(config, monitor, cancel, errCh),
		tea.WithAltScreen(),
	)
	return program.Run()
}

func finishRun(returnedModel tea.Model, tuiErr error, errCh <-chan error) error {
	serverErr := serverErrorFromModel(returnedModel)
	if serverErr == nil {
		serverErr = waitForServerError(errCh)
	}
	if tuiErr != nil {
		return tuiErr
	}
	if serverErr != nil && !isExpectedTUIServerStop(serverErr) {
		return serverErr
	}
	return nil
}

func serverErrorFromModel(returnedModel tea.Model) error {
	if model, ok := returnedModel.(trafficTUIModel); ok {
		return model.serverErr
	}
	return nil
}

func waitForServerError(errCh <-chan error) error {
	select {
	case err := <-errCh:
		return err
	case <-time.After(5 * time.Second):
		return nil
	}
}

func isExpectedTUIServerStop(err error) bool {
	return errors.Is(err, context.Canceled) ||
		errors.Is(err, http.ErrServerClosed) ||
		errors.Is(err, net.ErrClosed)
}
