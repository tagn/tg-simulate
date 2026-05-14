package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/tagn/tg-simulate/internal/graph"
	"github.com/tagn/tg-simulate/internal/report"
	"github.com/tagn/tg-simulate/internal/runner"
	"github.com/tagn/tg-simulate/internal/simulator"
)

func main() {
	root := &cobra.Command{
		Use:   "tg-simulate",
		Short: "Dependency-aware Terragrunt plan simulator",
	}

	root.AddCommand(newRunCmd())
	root.AddCommand(newExplainCmd())

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func newRunCmd() *cobra.Command {
	var (
		workingDir    string
		unit          string
		format        string
		outputFile    string
		mergeStrategy string
		concurrency   int
	)

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Simulate plans across a Terragrunt stack",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			g, err := graph.Load(ctx, workingDir)
			if err != nil {
				return fmt.Errorf("loading graph: %w", err)
			}

			rpt, _, err := runner.Simulate(ctx, g, runner.Options{
				WorkingDir:    workingDir,
				TargetUnit:    unit,
				MergeStrategy: mergeStrategy,
				Concurrency:   concurrency,
			})
			if err != nil {
				return err
			}

			w := os.Stdout
			if outputFile != "" {
				f, err := os.Create(outputFile)
				if err != nil {
					return fmt.Errorf("opening output file: %w", err)
				}
				defer f.Close()
				w = f
			}

			if err := rpt.Render(w, report.Format(format)); err != nil {
				return fmt.Errorf("rendering report: %w", err)
			}

			if rpt.HasErrors() {
				os.Exit(2)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&workingDir, "working-dir", ".", "Root of the Terragrunt stack")
	cmd.Flags().StringVar(&unit, "unit", "", "Plan only this unit and its ancestors")
	cmd.Flags().StringVar(&format, "format", "text", "Output format: text | markdown | json")
	cmd.Flags().StringVar(&outputFile, "output-file", "", "Write report to file instead of stdout")
	cmd.Flags().StringVar(&mergeStrategy, "merge-strategy", "shallow", "mock_outputs_merge_strategy_with_state value")
	cmd.Flags().IntVar(&concurrency, "concurrency", 0, "Max units planned in parallel (default: GOMAXPROCS)")

	return cmd
}

func newExplainCmd() *cobra.Command {
	var workingDir string

	cmd := &cobra.Command{
		Use:   "explain <output_name>",
		Short: "Trace where a simulated value came from",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			outputName := args[0]

			statePath := workingDir + "/.tg-simulate-cache/simulation.json"
			sim, err := simulator.Load(statePath)
			if err != nil {
				return fmt.Errorf("loading simulation state (run `tg-simulate run` first): %w", err)
			}

			found := false
			for unitPath, outputs := range sim.UnitOutputs {
				if out, ok := outputs[outputName]; ok {
					found = true
					fmt.Printf("unit:   %s\n", unitPath)
					fmt.Printf("output: %s\n", outputName)
					fmt.Printf("value:  %v\n", out.Value)
					fmt.Printf("source: %s\n", sourceLabel(out.Source))
					fmt.Printf("confidence: %.1f\n", out.Confidence)
				}
			}
			if !found {
				fmt.Fprintf(os.Stderr, "output %q not found in last simulation state\n", outputName)
				os.Exit(1)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&workingDir, "working-dir", ".", "Root of the Terragrunt stack")
	return cmd
}

func sourceLabel(s simulator.OutputSource) string {
	switch s {
	case simulator.SourceRealState:
		return "real state (🟢)"
	case simulator.SourcePlannedKnown:
		return "planned known (🟡)"
	case simulator.SourceSynthetic:
		return "synthetic (🔴)"
	default:
		return "unknown"
	}
}
