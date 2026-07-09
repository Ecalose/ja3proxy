package logutil

import "log/slog"

const componentKey = "component"

func WithComponent(component string, args ...any) *slog.Logger {
	return slog.With(componentArgs(component, args...)...)
}

func Debug(component string, msg string, args ...any) {
	slog.Debug(msg, componentArgs(component, args...)...)
}

func Info(component string, msg string, args ...any) {
	slog.Info(msg, componentArgs(component, args...)...)
}

func Warn(component string, msg string, args ...any) {
	slog.Warn(msg, componentArgs(component, args...)...)
}

func Error(component string, msg string, args ...any) {
	slog.Error(msg, componentArgs(component, args...)...)
}

func componentArgs(component string, args ...any) []any {
	componentArgs := make([]any, 0, len(args)+2)
	componentArgs = append(componentArgs, componentKey, component)
	return append(componentArgs, args...)
}
