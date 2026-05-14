// Package runner wires together the graph, planner, simulator, inject, and report
// packages into the end-to-end simulation loop.
package runner

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"sync"

	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"

	"github.com/tagn/tg-simulate/internal/graph"
	"github.com/tagn/tg-simulate/internal/inject"
	"github.com/tagn/tg-simulate/internal/planner"
	"github.com/tagn/tg-simulate/internal/report"
	"github.com/tagn/tg-simulate/internal/simulator"
)

// Options controls a simulation run.
type Options struct {
	WorkingDir    string // root of the Terragrunt stack
	TargetUnit    string // if non-empty, plan only this unit and its ancestors
	MergeStrategy string // mock_outputs_merge_strategy_with_state (default: shallow)
	// StatePath is where the simulation state JSON is persisted for the explain command.
	// Defaults to <WorkingDir>/.tg-simulate-cache/simulation.json when empty.
	StatePath string
	// Concurrency is the maximum number of units planned in parallel.
	// Defaults to runtime.GOMAXPROCS(0) when zero.
	Concurrency int
}

// planUnitFunc and generateOverlayFunc are package-level so tests can replace them.
var planUnitFunc = planner.PlanUnit
var generateOverlayFunc func(*graph.Unit, *simulator.Simulation, string, string, string) (string, error) = inject.GenerateOverlay

// Simulate runs the full simulation loop against g and returns the report and
// final simulation state. Units at the same DAG level are planned concurrently;
// a unit never starts until all its dependencies have been resolved.
//
// Per-unit errors are collected into the report rather than aborting the run.
// The returned error is non-nil only for fatal pre-loop failures (scratch dir
// creation, graph filtering). A run with per-unit errors still returns a valid
// report and a nil error.
func Simulate(ctx context.Context, g *graph.Graph, opts Options) (*report.Report, *simulator.Simulation, error) {
	if opts.MergeStrategy == "" {
		opts.MergeStrategy = "shallow"
	}
	concurrency := opts.Concurrency
	if concurrency <= 0 {
		concurrency = runtime.GOMAXPROCS(0)
	}

	// Apply --unit filter.
	if opts.TargetUnit != "" {
		var err error
		g, err = FilterGraph(g, opts.TargetUnit)
		if err != nil {
			return nil, nil, fmt.Errorf("filtering graph: %w", err)
		}
	}

	scratchDir, err := inject.NewScratchDir()
	if err != nil {
		return nil, nil, err
	}
	cancelSignal := inject.RegisterCleanup(scratchDir)
	defer cancelSignal()
	defer inject.Cleanup(scratchDir)

	sim := simulator.NewSimulation()
	rpt := report.NewReport()

	// Pre-build mock hints to avoid O(N²) per-unit scanning.
	prebuiltHints := buildMockHints(g)

	// Per-unit channel closed when the unit's outputs are written into sim.
	// Dependents wait on their deps' channels before starting.
	ready := make(map[string]chan struct{}, len(g.Order))
	for _, path := range g.Order {
		ready[path] = make(chan struct{})
	}

	var (
		simMu sync.RWMutex // guards all reads/writes of sim.UnitOutputs
		rptMu sync.Mutex   // guards rpt.AddUnit / rpt.AddError
	)

	sem := semaphore.NewWeighted(int64(concurrency))
	eg, egCtx := errgroup.WithContext(ctx)

	for _, unitPath := range g.Order {
		unitPath := unitPath
		unit := g.Units[unitPath]

		eg.Go(func() error {
			// Wait for all dependencies to finish (success or failure).
			for _, depPath := range unit.Dependencies {
				select {
				case <-ready[depPath]:
				case <-egCtx.Done():
					return egCtx.Err()
				}
			}

			if err := sem.Acquire(egCtx, 1); err != nil {
				return err
			}
			defer sem.Release(1)

			// Signal dependents when this goroutine exits, regardless of outcome.
			defer close(ready[unitPath])

			// 1. Generate overlay — reads sim under RLock so concurrent writers
			//    at the same level don't cause a map data race.
			simMu.RLock()
			overlayPath, err := generateOverlayFunc(unit, sim, scratchDir, opts.MergeStrategy, opts.WorkingDir)
			simMu.RUnlock()
			if err != nil {
				rptMu.Lock()
				rpt.AddError(unit, fmt.Errorf("overlay: %w", err))
				rptMu.Unlock()
				return nil
			}

			// 2. Run plan — no sim access.
			planResult, err := planUnitFunc(egCtx, unit, planner.PlanOptions{
				OverlayPath:   overlayPath,
				MergeStrategy: opts.MergeStrategy,
			})
			if err != nil {
				rptMu.Lock()
				rpt.AddError(unit, fmt.Errorf("plan: %w", err))
				rptMu.Unlock()
				return nil
			}

			// 3. Extract output deltas — no sim access.
			deltas, err := simulator.ExtractDeltas(planResult)
			if err != nil {
				rptMu.Lock()
				rpt.AddError(unit, fmt.Errorf("extract deltas: %w", err))
				rptMu.Unlock()
				return nil
			}

			// 4. Write resolved outputs into sim.
			simMu.Lock()
			sim.PopulateFromDeltas(unitPath, deltas, prebuiltHints[unitPath])
			simMu.Unlock()

			// 5. Read sim for confidence/input tracking (RLock; deps are stable,
			//    but sibling units may be writing their own entries).
			simMu.RLock()
			confidence := computeConfidence(unit, sim)
			simulatedInputs := collectSimulatedInputs(unit, sim)
			simMu.RUnlock()

			rptMu.Lock()
			rpt.AddUnit(unit, planResult, deltas, confidence, simulatedInputs)
			rptMu.Unlock()

			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return nil, nil, err
	}

	// Persist simulation state for the explain command.
	statePath := opts.StatePath
	if statePath == "" {
		statePath = filepath.Join(opts.WorkingDir, ".tg-simulate-cache", "simulation.json")
	}
	if err := sim.Save(statePath); err != nil {
		// Non-fatal: the report is still valid.
		fmt.Printf("warning: could not save simulation state: %v\n", err)
	}

	return rpt, sim, nil
}

// buildMockHints pre-computes a map of unitPath → {outputName → mock value}
// sourced from each unit's downstream dependents. Building this once avoids
// O(N²) scanning inside the hot simulation loop.
func buildMockHints(g *graph.Graph) map[string]map[string]interface{} {
	result := make(map[string]map[string]interface{}, len(g.Units))
	for unitPath, unit := range g.Units {
		hints := make(map[string]interface{})
		for _, dependentPath := range unit.Dependents {
			dep, ok := g.Units[dependentPath]
			if !ok || dep.Config == nil {
				continue
			}
			for _, depCfg := range dep.Config.Dependencies {
				if depCfg.ConfigPath != unitPath {
					continue
				}
				for outputName, mockVal := range depCfg.MockOutputs {
					if _, exists := hints[outputName]; !exists {
						hints[outputName] = mockVal
					}
				}
			}
		}
		result[unitPath] = hints
	}
	return result
}

// MockHintsFor returns a map of output-name → existing mock value for unit,
// sourced from the dependency configs of unit's downstream dependents.
// These hints guide the synthetic generator toward the right value shape.
func MockHintsFor(unit *graph.Unit, g *graph.Graph) map[string]interface{} {
	return buildMockHints(g)[unit.Path]
}

// collectSimulatedInputs returns sorted "label.outputName" strings for every
// synthetic upstream output this unit received as a mock input.
func collectSimulatedInputs(unit *graph.Unit, sim *simulator.Simulation) []string {
	if unit.Config == nil {
		return nil
	}
	var inputs []string
	for label, dep := range unit.Config.Dependencies {
		for outputName, simOut := range sim.UnitOutputs[dep.ConfigPath] {
			if simOut.Source == simulator.SourceSynthetic {
				inputs = append(inputs, label+"."+outputName)
			}
		}
	}
	sort.Strings(inputs)
	return inputs
}

// computeConfidence determines the confidence level for a unit based on the
// sources of its upstream inputs.
func computeConfidence(unit *graph.Unit, sim *simulator.Simulation) report.Confidence {
	if unit.Config == nil || len(unit.Config.Dependencies) == 0 {
		return report.ConfidenceReal
	}
	hasSynthetic := false
	hasReal := false
	for _, dep := range unit.Config.Dependencies {
		for _, simOut := range sim.UnitOutputs[dep.ConfigPath] {
			if simOut.Source == simulator.SourceSynthetic {
				hasSynthetic = true
			} else {
				hasReal = true
			}
		}
	}
	switch {
	case hasSynthetic && hasReal:
		return report.ConfidencePartial
	case hasSynthetic:
		return report.ConfidenceSynthetic
	default:
		return report.ConfidenceReal
	}
}
