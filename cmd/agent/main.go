package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"testTask/internal/app"
	"testTask/internal/config"
	"testTask/internal/repl"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	application, err := app.New(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize app: %w", err)
	}
	defer application.Close(context.Background())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		cancel()
	}()

	replInstance := repl.New(application.GetAgent(), application.GetLogger())
	if err := replInstance.Run(ctx); err != nil {
		return fmt.Errorf("REPL error: %w", err)
	}

	return nil
}
