package main

import (
	"log/slog"
	"net/http"
	"os"

	"mini-cloud/internal/controlplane"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	plane, err := controlplane.Build(logger)
	if err != nil {
		logger.Error("failed to build control-plane", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := plane.Close(); err != nil {
			logger.Warn("close control-plane failed", "error", err)
		}
	}()

	logger.Info("starting mini-cloud control-plane",
		"http_addr", plane.Config.HTTPAddr,
		"ui_dir", plane.Config.UIDir,
	)

	if err := http.ListenAndServe(plane.Config.HTTPAddr, plane.Handler); err != nil {
		logger.Error("control-plane exited", "error", err)
		os.Exit(1)
	}
}
