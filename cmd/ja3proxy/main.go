package main

import (
	"log/slog"
	"os"

	cflog "github.com/cloudflare/cfssl/log"
)

func init() {
	cflog.Level = cflog.LevelWarning
	configureDefaultLogger(slog.LevelInfo)
}

func main() {
	if err := newDefaultApp().run(); err != nil {
		slog.Error("run failed", "component", "runtime", "err", err)
		os.Exit(1)
	}
}
