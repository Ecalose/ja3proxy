package main

import (
	"log/slog"
	"os"

	"github.com/lylemi/ja3proxy/internal/ja3proxy"
)

func main() {
	if err := ja3proxy.Run(); err != nil {
		slog.Error("run failed", "component", "runtime", "err", err)
		os.Exit(1)
	}
}
