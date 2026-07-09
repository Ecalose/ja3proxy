package main

import (
	"os"

	"github.com/lylemi/ja3proxy/internal/ja3proxy"
	"github.com/lylemi/ja3proxy/internal/ja3proxy/logutil"
)

func main() {
	if err := ja3proxy.Run(); err != nil {
		logutil.Error("runtime", "run failed", "err", err)
		os.Exit(1)
	}
}
