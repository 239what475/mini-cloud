package main

import (
	"context"
	"fmt"
	"os"

	"mini-cloud/internal/lab"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		printUsage()
		return nil
	}

	command := args[0]
	cfgPath := "deploy/lab/lab.yaml"
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--config", "-config":
			if i+1 >= len(args) {
				return fmt.Errorf("%s requires a path", args[i])
			}
			cfgPath = args[i+1]
			i++
		case "-h", "--help":
			printUsage()
			return nil
		default:
			return fmt.Errorf("unknown argument %q", args[i])
		}
	}

	cfg, err := lab.LoadConfig(cfgPath)
	if err != nil {
		return err
	}
	runner := lab.NewRunner(cfg)

	switch command {
	case "bootstrap":
		return runner.Bootstrap(ctx)
	case "install":
		return runner.Install(ctx)
	case "destroy":
		return runner.Destroy(ctx)
	default:
		return fmt.Errorf("unknown command %q", command)
	}
}

func printUsage() {
	fmt.Println(`mini-cloud labctl

Usage:
  labctl bootstrap [--config deploy/lab/lab.yaml]
  labctl install   [--config deploy/lab/lab.yaml]
  labctl destroy   [--config deploy/lab/lab.yaml]`)
}
