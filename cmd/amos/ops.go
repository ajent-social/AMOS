package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/ajent-social/amos/internal/scaffold/ops"
)

func runOps(args []string) int { return runOpsIO(args, os.Stdout, os.Stderr) }
func runOpsIO(args []string, out, diagnostics io.Writer) int {
	fail := func() int {
		_, _ = fmt.Fprintln(diagnostics, "amos ops: invalid input or operation unavailable")
		return 2
	}
	if len(args) == 0 {
		return fail()
	}
	flags := flag.NewFlagSet("ops", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	config := flags.String("config", "", "")
	definition := flags.String("definition", "", "")
	samples := flags.String("samples", "", "")
	if flags.Parse(args[1:]) != nil || flags.NArg() != 0 {
		return fail()
	}
	var body []byte
	switch args[0] {
	case "render":
		if *config == "" || *definition != "" || *samples != "" {
			return fail()
		}
		data, err := readOpsFile(*config, ops.MaxDefinitionBytes)
		if err != nil {
			return fail()
		}
		cfg, err := ops.DecodeConfig(data)
		if err != nil {
			return fail()
		}
		bundle, err := ops.Generate(cfg)
		if err != nil {
			return fail()
		}
		body, err = ops.Encode(bundle)
		if err != nil {
			return fail()
		}
	case "evaluate":
		if *definition == "" || *samples == "" || *config != "" {
			return fail()
		}
		data, err := readOpsFile(*definition, ops.MaxDefinitionBytes)
		if err != nil {
			return fail()
		}
		bundle, err := ops.DecodeBundle(data)
		if err != nil {
			return fail()
		}
		data, err = readOpsFile(*samples, ops.MaxSampleBytes)
		if err != nil {
			return fail()
		}
		input, err := ops.DecodeSamples(data)
		if err != nil {
			return fail()
		}
		result, err := ops.Evaluate(bundle, input)
		if err != nil {
			return fail()
		}
		body, err = json.Marshal(result)
		if err != nil {
			return fail()
		}
	default:
		return fail()
	}
	if _, err := out.Write(append(body, '\n')); err != nil {
		return fail()
	}
	return 0
}
func readOpsFile(path string, limit int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, ops.ErrInvalid
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil || len(data) > limit {
		return nil, ops.ErrInvalid
	}
	return data, nil
}
