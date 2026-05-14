# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

`tg-simulate` is a Go CLI that produces a dependency-aware plan preview across a Terragrunt stack by simulating how upstream output changes propagate to downstream units. It shells out to `terragrunt` and `terraform`, never modifies state, and never runs apply.

## Commands

```bash
# Build
go build ./cmd/tg-simulate/...

# Run
go run ./cmd/tg-simulate/... run --working-dir ./testdata/simple-chain

# Test all
go test ./...

# Test a single package
go test ./internal/graph/...

# Test a single function
go test ./internal/graph/... -run TestTopologicalSort

# Lint
golangci-lint run ./...

# Generate mocks (if needed)
go generate ./...
```

## Architecture

The tool processes a Terragrunt stack in five stages that map directly to the package layout:

```
cmd/tg-simulate/     CLI entry point (cobra + viper)
internal/graph/      DAG extraction and topological sort
internal/planner/    Per-unit plan execution (shells out to terragrunt + terraform)
internal/simulator/  Output delta extraction and synthetic value generation
internal/inject/     HCL overlay generation for mock_outputs injection
internal/report/     Aggregation into text/markdown/JSON output
pkg/tfplan/          Terraform plan JSON structs (wraps hashicorp/terraform-json)
testdata/            Fixture stacks using null_resource — simple-chain, diamond, greenfield, with-changes
```

### Data flow

1. **Graph** — `terragrunt dag graph` → DOT parse → `Graph{Units, Order}` (Kahn's topological sort)
2. **Planner** — for each unit in topological order: write HCL overlay with simulated upstream outputs → `terragrunt plan -out=binary` → `terraform show -json` → `PlanResult`
3. **Simulator** — extract `OutputDelta` per output; for `AfterUnknown: true` values, call `SyntheticGenerator` keyed on name heuristics and existing `mock_outputs` shape
4. **Inject** — write per-unit overlay using `hclwrite`; overlays live in a per-run scratch dir and are never written to the original source tree
5. **Report** — aggregate `UnitReport` entries; emit text/markdown/JSON with confidence indicators (🟢 real / 🟡 partially simulated / 🔴 fully synthetic)

### Key invariants

- **Never mutate source files.** All overlays go to `<tmpdir>/<run-id>/`.
- **Tag every synthetic value.** Use a `sim-` prefix so reviewers know what's real.
- **`mock_outputs_merge_strategy_with_state`** defaults to `"shallow"` (real state wins); expose `--merge-strategy` to override.
- Sensitive outputs arrive as `null` from `terraform show -json`; the report must not attempt to recover or log them.

### Parallelism

v1 is strictly sequential (one unit at a time in topological order). Parallelism across same-level DAG nodes is a planned optimization using `errgroup` with bounded concurrency.

## Key dependencies

| Package | Purpose |
|---|---|
| `github.com/spf13/cobra` | CLI commands |
| `github.com/spf13/viper` | Config (flags + env + file) |
| `github.com/hashicorp/terraform-json` | Terraform plan JSON structs |
| `github.com/hashicorp/hcl/v2` + `hclwrite` | HCL parsing and overlay generation |
| `github.com/awalterschulze/gographviz` | DOT format parsing |
| `golang.org/x/sync/errgroup` | Bounded concurrency (future) |

## Testing

Integration tests run real `terragrunt` and `terraform` binaries against fixture stacks in `testdata/`. Each fixture uses `null_resource` and local providers — no cloud credentials required. The critical test is a scenario where an upstream `force_new` attribute change causes visible downstream resource replacement in the simulated plan.

Unit tests cover: graph parsing (cycles, missing deps, malformed DOT), synthetic generators, overlay HCL generation (golden-file fixtures), and plan JSON edge cases.

## Risks to keep in mind

- Some providers reject malformed IDs at plan time even in mock contexts — synthetic generators may need provider-specific logic (Azure vs AWS vs GCP).
- `terragrunt render-json` is called per unit; cache results within a run.
- Pin a minimum Terragrunt version — DAG command names and config rendering have changed across releases.
