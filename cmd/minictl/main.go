package main

import (
	"context"
	"fmt"
	"os"

	"mini-cloud/internal/ops"
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
	cfgPath := "deploy/ops/config.yaml"
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

	cfg, err := ops.LoadConfig(cfgPath)
	if err != nil {
		return err
	}
	runner := ops.NewRunner(cfg)

	switch command {
	case "check":
		return runner.Check(ctx)
	case "build":
		return runner.Build(ctx)
	case "bootstrap":
		return runner.Bootstrap(ctx)
	case "install":
		return runner.Install(ctx)
	case "update":
		return runner.Update(ctx)
	case "deploy":
		return runner.Deploy(ctx)
	case "e2e":
		return runner.E2E(ctx)
	case "destroy":
		return runner.Destroy(ctx)
	default:
		return fmt.Errorf("unknown command %q", command)
	}
}

func printUsage() {
	fmt.Println(`mini-cloud minictl

Usage:
  minictl check                [--config deploy/ops/config.yaml]
  minictl build                [--config deploy/ops/config.yaml]
  minictl bootstrap            [--config deploy/ops/config.yaml]
  minictl install              [--config deploy/ops/config.yaml]
  minictl update               [--config deploy/ops/config.yaml]
  minictl deploy               [--config deploy/ops/config.yaml]
  minictl e2e                  [--config deploy/ops/config.yaml]
  minictl destroy              [--config deploy/ops/config.yaml]

The ops config contains one control-plane and one or more cloud-plane entries.`)
}
