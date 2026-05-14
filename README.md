# tg-simulate

A CLI that produces a dependency-aware plan preview across a Terragrunt stack by simulating how
upstream output changes propagate to downstream units — without touching state or running apply.

> [!CAUTION] Never use any generated tfplan artifacts from this tool in apply operations.
> This tool provides an estimation of expected changes. It does not solve any issues with
> terragrunt creating plans that contain mock or unknown-until-apply values.

## Problem

`terragrunt run --all plan` plans every unit against the *current* state outputs of its
dependencies. If unit **A** is about to be replaced (force-new), unit **B** (which depends on A)
plans against A's *old* output — not the future one. The blast radius of a change is hidden until
apply time.

`tg-simulate` closes that gap:

1. Plans units in topological order.
2. Extracts the output deltas from each plan.
3. Injects those values (real or synthetic) as `mock_outputs` into downstream dependency blocks 
  before planning them.

The result is a single report that reveals the full cascade of a change across the stack.

## Prerequisites

| Tool | Minimum version | Notes |
|------|----------------|-------|
| Go | 1.22 | build from source only |
| `terragrunt` | 1.0.0 | must be on `$PATH` |
| `terraform` or `tofu` | any | must be on `$PATH`; set `TF_BINARY=tofu` to prefer OpenTofu |

## Install

**From source (recommended until a release is published):**

```bash
git clone https://github.com/tagn/tg-simulate
cd tg-simulate
go build -o tg-simulate ./cmd/tg-simulate/
```

**Via `go install`:**

```bash
go install github.com/tagn/tg-simulate/cmd/tg-simulate@latest
```

## Usage

```
tg-simulate run [--working-dir DIR] [--unit PATH] [--format text|markdown|json] [--output-file FILE]

tg-simulate explain <output_name> [--working-dir DIR]
```

### `run` — simulate a stack

```bash
# Plan every unit in the stack (current directory)
tg-simulate run

# Specify the stack root explicitly
tg-simulate run --working-dir ./live/prod

# Plan only one unit and its transitive dependencies
tg-simulate run --unit ./live/prod/app-service

# Emit a markdown report and save it to a file
tg-simulate run --working-dir ./live/prod --format markdown --output-file plan.md

# Emit machine-readable JSON
tg-simulate run --working-dir ./live/prod --format json
```

### `explain` — trace a simulated value

After a `run`, the simulation state is persisted to `.tg-simulate-cache/simulation.json`. The
`explain` command looks up how a named output was derived.

```bash
tg-simulate explain resource_id
# unit:       /live/prod/networking
# output:     resource_id
# value:      sim-3f7a1c...
# source:     synthetic (🔴)
# confidence: 0.3
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--working-dir` | `.` | Root directory of the Terragrunt stack |
| `--unit` | _(all units)_ | Plan only this unit and its ancestors |
| `--format` | `text` | Output format: `text`, `markdown`, `json` |
| `--output-file` | _(stdout)_ | Write the report to a file instead of stdout |
| `--concurrency` | _(GOMAXPROCS)_ | Maximum number of units planned in parallel |

## Hands-on walkthrough

The repository ships with fixture stacks under `testdata/`. They use `null_resource` and a
local backend — no cloud credentials needed.

### Build and point at the simple-chain fixture

```bash
go build -o tg-simulate ./cmd/tg-simulate/
./tg-simulate run --working-dir ./testdata/simple-chain
```

**What this stack looks like:**

```
a  (no dependencies, produces resource_id)
└─▶ b  (depends on a.resource_id, produces resource_id)
    └─▶ c  (depends on b.resource_id, produces resource_id)
```

**Output (simple-chain — no state applied yet):**

```
=== tg-simulate Report ===

Summary: 3 unit(s) planned
  🟢 real: 1  🟡 partial: 0  🔴 synthetic: 2

--- simple-chain/a (🟢 real) ---
  Resources: +1 ~0 -0
    + null_resource.this
  Outputs:
    resource_id                    [create ]  (unknown)

--- simple-chain/b (🔴 fully synthetic) ---
  Resources: +1 ~0 -0
    + null_resource.this
  Outputs:
    resource_id                    [create ]  (unknown)
  Simulated inputs: a.resource_id
  ⚠️  Uses synthetic upstream values — some diffs may resolve to no-op at apply

--- simple-chain/c (🔴 fully synthetic) ---
  Resources: +1 ~0 -0
    + null_resource.this
  Outputs:
    resource_id                    [create ]  (unknown)
  Simulated inputs: b.resource_id
  ⚠️  Uses synthetic upstream values — some diffs may resolve to no-op at apply
```

Unit **a** is 🟢 because it has no upstream inputs — its plan is exact. Units **b** and **c** are 🔴
because `resource_id` for a `null_resource` is `(known after apply)`, so `tg-simulate` generates a
synthetic placeholder (`sim-…`) to stand in for the real value. Their plans are directionall
 correct — you can see the creates — but the specific ID values will differ at real apply time.

### Simulating upstream change propagation

The `testdata/with-changes` fixture demonstrates force-new propagation. Unit **a** has a
`triggers.version` value; unit **b** uses `a.resource_id` as its own trigger. When `version`
changes, **a** is replaced, which means **a**'s `resource_id` changes, which forces **b** to be
replaced too.

**Step 1 — apply state to establish a baseline:**

```bash
TMPDIR=$(mktemp -d)
cp -r ./testdata/with-changes/. "$TMPDIR/"

TG_TF_PATH=tofu terragrunt run --all apply --non-interactive \
  --working-dir "$TMPDIR"
```

**Step 2 — simulate before any code change:**

```bash
./tg-simulate run --working-dir "$TMPDIR"
```

```
=== tg-simulate Report ===

Summary: 2 unit(s) planned
  🟢 real: 2  🟡 partial: 0  🔴 synthetic: 0

--- .../a (🟢 real) ---
  Resources: +0 ~0 -0
  Outputs:
    resource_id                    [no-op  ]  3390479182350919450

--- .../b (🟢 real) ---
  Resources: +0 ~0 -0
```

Both units are stable. No changes. Confidence is 🟢 because all inputs come from real state.

**Step 3 — change `a`'s trigger from `"v1"` to `"v2"`:**

```bash
sed -i '' 's/"v1"/"v2"/' "$TMPDIR/a/main.tf"
```

**Step 4 — simulate again:**

```bash
./tg-simulate run --working-dir "$TMPDIR"
```

```
=== tg-simulate Report ===

Summary: 2 unit(s) planned
  🟢 real: 1  🟡 partial: 0  🔴 synthetic: 1

--- .../a (🟢 real) ---
  Resources: +1 ~0 -1
    ± null_resource.this
  Outputs:
    resource_id                    [update ]  (unknown)

--- .../b (🔴 fully synthetic) ---
  Resources: +1 ~0 -1
    ± null_resource.this
  Outputs:
    resource_id                    [update ]  (unknown)
  Simulated inputs: a.resource_id
  ⚠️  Uses synthetic upstream values — some diffs may resolve to no-op at apply
```

`tg-simulate` detected that:
- **a** will be replaced (`+1 ~0 -1`) — the trigger changed.
- **a**'s `resource_id` output is unknown after apply (a new random ID will be assigned).
- **b** is planned with a synthetic stand-in for the new `resource_id`, revealing that **b** will
  also be replaced.

This is the core value of `tg-simulate`: **b**'s replacement is invisible to a plain
`terragrunt run --all plan`.

## Understanding the output

### Confidence levels

| Symbol | Label | Meaning |
|--------|-------|---------|
| 🟢 | real | All upstream inputs came from current Terraform state or a known planned value. The plan is exact. |
| 🟡 | partially simulated | At least one upstream input was real, at least one was synthetic. The plan is directionally correct. |
| 🔴 | fully synthetic | All upstream inputs were generated heuristically. The plan shows the right *structure* of changes, but specific values may differ at apply time. |

### Resource change summary

```
Resources: +add ~change -destroy
```

A `-1 +1` pair (one destroy, one create) indicates a resource replacement (force-new).

### Output deltas

| Action | Meaning |
|--------|---------|
| `create` | Output did not exist before and will be set |
| `update` | Output value will change |
| `delete` | Output will be removed |
| `no-op` | Output is unchanged (shown for reference) |

### Synthetic values

When an upstream output is `(known after apply)`, `tg-simulate` generates a synthetic stand-in
prefixed with `sim-`. These values are passed as `mock_outputs` into downstream dependency blocks
so the downstream plan can execute. The 🔴/🟡 confidence label and the `⚠️` warning flag any unit
where you should treat the plan as indicative rather than exact.

### Exit codes

| Code | Meaning |
|------|---------|
| 0 | All units planned successfully |
| 1 | Fatal error (graph load failure, flag error) |
| 2 | One or more units encountered a plan error (report is still emitted) |

## How it works

```
terragrunt dag graph          →  dependency graph (topological sort)
  ↓ for each unit in order:
terragrunt plan -out=plan.bin →  binary plan file
terraform show -json          →  plan JSON
  ↓
extract output deltas         →  real values where known, synthetic where (known after apply)
inject as mock_outputs        →  patch dependency blocks in a scratch copy of terragrunt.hcl
  ↓ continue to next unit
```

Overlays are written to a temporary scratch directory. The original source tree is never modified.

`mock_outputs_merge_strategy_with_state` is set to `no_merge` for every injected dependency so that
simulated future values take precedence over any current state that may exist for the upstream unit.

## Caveats

- **`(known after apply)` values** produce synthetic stand-ins. A downstream unit's plan may show 
  diffs that collapse to no-ops at real apply time. Always read the confidence label.
- **Sensitive outputs** arrive redacted from `terraform show -json`. They are passed through as a
  synthetic placeholder; the report never logs the actual value.
- **Provider validation at plan time.** Some providers validate resource IDs during plan. If a
  provider rejects a `sim-…` placeholder, the unit will report an error. This is expected behaviour and is surfaced in the report with exit code 2.
- **Parallel execution.** Units at the same DAG level are planned concurrently (bounded by 
  `--concurrency`, defaulting to `GOMAXPROCS`). A unit never starts until all its direct
  dependencies have been resolved, so ordering is always correct. On large stacks, setting 
  `--concurrency 1` restores fully sequential behaviour.
- **Terragrunt ≥ 1.0.0 required.** The tool uses `terragrunt dag graph`, `terragrunt render --json`,
  and `TG_TF_PATH` — all of which changed or were introduced in 1.0.x.

## Development

```bash
# Build
go build -o tg-simulate ./cmd/tg-simulate/

# Unit tests
go test ./...

# Integration tests (requires terragrunt + terraform or tofu on PATH)
go test -tags integration -timeout 10m ./internal/runner/...

# Lint
golangci-lint run ./...
```

## License

MIT — see [LICENSE](LICENSE).
