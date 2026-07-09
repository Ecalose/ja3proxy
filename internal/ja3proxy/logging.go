package ja3proxy

import (
	"log/slog"
	"os"

	cflog "github.com/cloudflare/cfssl/log"
)

func init() {
	cflog.Level = cflog.LevelWarning
	configureDefaultLogger(slog.LevelInfo)
}

func configureDefaultLogger(level slog.Level) {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	})))
}
