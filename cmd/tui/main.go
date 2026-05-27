package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"mini-cloud/internal/common/util"
	"mini-cloud/internal/operatorclient"
	tuiapp "mini-cloud/internal/tui/app"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	var (
		grpcEndpoint     = flag.String("addr", getenv("MINICLOUD_CONTROL_PLANE_URL", "http://127.0.0.1:8080"), "control-plane URL")
		adminToken       = flag.String("token", getenv("MINICLOUD_ADMIN_TOKEN", ""), "operator bearer token")
		initialProjectID = flag.String("project", "", "initial project ID for the Services tab")
	)
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	client, err := operatorclient.New(*grpcEndpoint, *adminToken)
	if err != nil {
		logger.Error("failed to create operator client", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := client.Close(); err != nil {
			logger.Warn("close operator client failed", "error", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	program := tea.NewProgram(
		tuiapp.New(client, tuiapp.Options{InitialProjectID: strings.TrimSpace(*initialProjectID)}),
		tea.WithContext(ctx),
		tea.WithAltScreen(),
	)

	if _, err := program.Run(); err != nil {
		util.Fprintf(os.Stderr, "run tui: %v\n", err)
		os.Exit(1)
	}
}

func getenv(key string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
