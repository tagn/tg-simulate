package runner

import (
	"fmt"
	"path/filepath"

	"github.com/tagn/tg-simulate/internal/graph"
)

// FilterGraph returns a new Graph containing only targetPath and its transitive
// dependencies (ancestors), preserving the original topological order.
// targetPath may be relative; it is resolved to absolute before lookup.
func FilterGraph(g *graph.Graph, targetPath string) (*graph.Graph, error) {
	abs, err := filepath.Abs(targetPath)
	if err != nil {
		return nil, fmt.Errorf("resolving target path: %w", err)
	}
	if _, ok := g.Units[abs]; !ok {
		return nil, fmt.Errorf("unit %q not found in graph", abs)
	}

	// BFS upstream: collect the target unit and all its ancestors.
	visited := make(map[string]bool)
	queue := []string{abs}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if visited[cur] {
			continue
		}
		visited[cur] = true
		if u, ok := g.Units[cur]; ok {
			for _, dep := range u.Dependencies {
				if !visited[dep] {
					queue = append(queue, dep)
				}
			}
		}
	}

	// Build the subgraph, preserving the original topological order.
	subUnits := make(map[string]*graph.Unit, len(visited))
	subOrder := make([]string, 0, len(visited))
	for _, path := range g.Order {
		if visited[path] {
			subUnits[path] = g.Units[path]
			subOrder = append(subOrder, path)
		}
	}

	return &graph.Graph{Units: subUnits, Order: subOrder}, nil
}
