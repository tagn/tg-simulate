package graph

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/awalterschulze/gographviz"
)

// extractDOT shells out to `terragrunt dag graph` and returns the DOT output.
func extractDOT(ctx context.Context, workingDir string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "terragrunt", "dag", "graph", "--working-dir", workingDir)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("terragrunt dag graph: %w", err)
	}
	return out, nil
}

// parseDOT builds a Graph from DOT bytes produced by terragrunt dag graph.
// workingDir is used to resolve relative node paths to absolute paths.
func parseDOT(dot []byte, workingDir string) (*Graph, error) {
	parsed, err := gographviz.Read(dot)
	if err != nil {
		return nil, fmt.Errorf("parsing DOT: %w", err)
	}

	units := make(map[string]*Unit)

	// Collect all nodes first so isolated units (no edges) are included.
	for name := range parsed.Nodes.Lookup {
		abs := resolvePath(unquote(name), workingDir)
		if _, ok := units[abs]; !ok {
			units[abs] = &Unit{
				Path:         abs,
				Dependencies: []string{},
				Dependents:   []string{},
			}
		}
	}

	// Process edges: in terragrunt DOT output, Src -> Dst means Src depends on Dst.
	for _, edge := range parsed.Edges.Edges {
		srcPath := resolvePath(unquote(edge.Src), workingDir)
		dstPath := resolvePath(unquote(edge.Dst), workingDir)

		// Ensure both nodes exist (they should, but be defensive).
		if _, ok := units[srcPath]; !ok {
			units[srcPath] = &Unit{Path: srcPath, Dependencies: []string{}, Dependents: []string{}}
		}
		if _, ok := units[dstPath]; !ok {
			units[dstPath] = &Unit{Path: dstPath, Dependencies: []string{}, Dependents: []string{}}
		}

		units[srcPath].Dependencies = append(units[srcPath].Dependencies, dstPath)
		units[dstPath].Dependents = append(units[dstPath].Dependents, srcPath)
	}

	// Sort dependency/dependent slices for determinism.
	for _, u := range units {
		sort.Strings(u.Dependencies)
		sort.Strings(u.Dependents)
	}

	order, err := topoSort(units)
	if err != nil {
		return nil, err
	}

	return &Graph{Units: units, Order: order}, nil
}

// topoSort returns a topological ordering of units using Kahn's algorithm.
// Returns an error listing the cycle members if the graph contains a cycle.
func topoSort(units map[string]*Unit) ([]string, error) {
	// inDegree = number of unprocessed dependencies for each unit.
	inDegree := make(map[string]int, len(units))
	// adj maps a unit to the list of units that depend on it.
	adj := make(map[string][]string, len(units))

	for path, u := range units {
		if _, ok := inDegree[path]; !ok {
			inDegree[path] = 0
		}
		for _, dep := range u.Dependencies {
			inDegree[path]++
			adj[dep] = append(adj[dep], path)
		}
	}

	// Seed the queue with all units that have no dependencies.
	// Use a sorted slice for deterministic output.
	var queue []string
	for path, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, path)
		}
	}
	sort.Strings(queue)

	order := make([]string, 0, len(units))
	head := 0
	for head < len(queue) {
		cur := queue[head]
		head++
		order = append(order, cur)

		// Reduce in-degree for each node that depends on cur.
		next := make([]string, 0)
		for _, dependent := range adj[cur] {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				next = append(next, dependent)
			}
		}
		sort.Strings(next)
		queue = append(queue, next...)
	}

	if len(order) != len(units) {
		// Collect nodes still stuck in the cycle.
		var cycle []string
		for path, deg := range inDegree {
			if deg > 0 {
				cycle = append(cycle, path)
			}
		}
		sort.Strings(cycle)
		return nil, fmt.Errorf("dependency cycle detected among units: %s", strings.Join(cycle, ", "))
	}

	return order, nil
}

// unquote strips surrounding double-quotes that gographviz preserves for quoted DOT identifiers.
func unquote(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

// resolvePath converts a node path (possibly relative) to an absolute path
// anchored at workingDir, then resolves symlinks so that paths that refer to
// the same filesystem location compare equal regardless of how they were written
// (e.g. /var/... vs /private/var/... on macOS where /var → /private/var).
func resolvePath(p, workingDir string) string {
	var abs string
	if filepath.IsAbs(p) {
		abs = filepath.Clean(p)
	} else {
		abs = filepath.Clean(filepath.Join(workingDir, p))
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return abs
}
