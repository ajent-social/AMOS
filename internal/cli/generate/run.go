// Package generate implements the command handler used by the root CLI.
// Executable registration belongs to the repository integrator.
package generate

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/ajent-social/amos/internal/codegen"
)

// Run validates an OpenAPI file and prints or atomically writes its stable
// generation manifest. The root executable can delegate `amos generate` here.
func Run(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("generate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	specPath := flags.String("spec", "", "OpenAPI 3.1.1 source file")
	manifestPath := flags.String("manifest", "", "optional generated manifest output path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", flags.Args())
	}
	if *specPath == "" {
		return errors.New("--spec is required")
	}
	source, err := os.ReadFile(*specPath)
	if err != nil {
		return errors.New("unable to read OpenAPI source")
	}
	manifest, err := codegen.Validate(source)
	if err != nil {
		return err
	}
	output, err := codegen.RenderManifest(manifest)
	if err != nil {
		return err
	}
	if *manifestPath == "" {
		if _, err := stdout.Write(output); err != nil {
			return fmt.Errorf("write generation manifest to stdout: %w", err)
		}
		return nil
	}
	if err := codegen.WriteManifest(*manifestPath, output); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(stdout, "validated %d operation(s); manifest written\n", len(manifest.Operations)); err != nil {
		return fmt.Errorf("write generation status: %w", err)
	}
	return nil
}
