package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/ajent-social/amos/internal/initializer"
	"github.com/ajent-social/amos/internal/scaffold/application"
)

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	if len(args) == 0 || args[0] != "init" {
		fmt.Fprintln(os.Stderr, "usage: amos init --config <file> --framework-source <trusted-public-repo>")
		return 2
	}
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	config := flags.String("config", "", "initializer JSON file")
	resume := flags.String("resume", "", "owned staging directory basename to resume")
	source := flags.String("framework-source", "", "trusted public AMOS source checkout")
	if flags.Parse(args[1:]) != nil || flags.NArg() != 0 || *config == "" || *source == "" {
		fmt.Fprintln(os.Stderr, "usage: amos init --config <file> --framework-source <trusted-public-repo>")
		return 2
	}
	f, e := os.Open(*config)
	if e != nil {
		fmt.Fprintln(os.Stderr, "amos init: cannot read configuration")
		return 2
	}
	defer func() { _ = f.Close() }()
	input, e := initializer.DecodeInput(f)
	if e != nil {
		fmt.Fprintln(os.Stderr, "amos init: invalid configuration")
		return 2
	}
	if input.Mode == "evaluation" && input.BusinessMode != "integrated-go" {
		fmt.Fprintln(os.Stderr, "amos init: requested business profile is unavailable")
		return 3
	}
	if input.Mode == "production-target" {
		fmt.Fprintln(os.Stderr, "amos init: production generation is unavailable")
		return 3
	}
	modules := make(map[string]bool, len(input.Modules))
	for _, module := range input.Modules {
		modules[module] = true
	}
	if !modules["identity"] || !modules["workspace"] {
		fmt.Fprintln(os.Stderr, "amos init: evaluation requires identity and workspace modules")
		return 2
	}
	if modules["billing"] {
		fmt.Fprintln(os.Stderr, "amos init: billing is unavailable in local evaluation")
		return 3
	}
	if application.ValidateSource(*source) != nil {
		fmt.Fprintln(os.Stderr, "amos init: trusted framework source is unavailable")
		return 3
	}
	var result initializer.Result
	if *resume == "" {
		result, e = initializer.Initialize(context.Background(), input, application.Generator{SourceDir: *source})
	} else {
		result, e = initializer.Resume(context.Background(), input, *resume, application.Generator{SourceDir: *source})
	}
	if e != nil {
		fmt.Fprintln(os.Stderr, "amos init: generation failed")
		if result.StagingName != "" {
			fmt.Fprintf(os.Stderr, "owned staging directory: %s\n", result.StagingName)
		}
		switch {
		case errors.Is(e, initializer.ErrInvalidInput), errors.Is(e, initializer.ErrTargetConflict), errors.Is(e, initializer.ErrResumeConflict):
			return 2
		case errors.Is(e, initializer.ErrGeneratorUnavailable):
			return 3
		default:
			return 4
		}
	}
	if _, err := fmt.Fprintf(os.Stdout, "created %s\n", input.Target); err != nil {
		return 4
	}
	return 0
}
