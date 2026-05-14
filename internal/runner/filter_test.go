package runner

import (
	"path/filepath"
	"testing"

	"github.com/tagn/tg-simulate/internal/graph"
)

// buildGraph constructs a Graph with the given edges (src depends on dst).
// All unit paths are made absolute using t.TempDir() as a root.
func buildGraph(t *testing.T, edges [][2]string, extraNodes ...string) (*graph.Graph, string) {
	t.Helper()
	root := t.TempDir()

	abs := func(rel string) string { return filepath.Join(root, rel) }

	units := map[string]*graph.Unit{}
	ensure := func(rel string) {
		p := abs(rel)
		if _, ok := units[p]; !ok {
			units[p] = &graph.Unit{
				Path:         p,
				Dependencies: []string{},
				Dependents:   []string{},
				Config:       &graph.TerragruntConfig{Dependencies: map[string]*graph.DependencyConfig{}},
			}
		}
	}

	for _, e := range edges {
		ensure(e[0])
		ensure(e[1])
	}
	for _, n := range extraNodes {
		ensure(n)
	}

	for _, e := range edges {
		src, dst := abs(e[0]), abs(e[1])
		units[src].Dependencies = append(units[src].Dependencies, dst)
		units[dst].Dependents = append(units[dst].Dependents, src)
	}

	// Build a simple topological order (roots first).
	inDeg := map[string]int{}
	adj := map[string][]string{}
	for p, u := range units {
		if _, ok := inDeg[p]; !ok {
			inDeg[p] = 0
		}
		for _, dep := range u.Dependencies {
			inDeg[p]++
			adj[dep] = append(adj[dep], p)
		}
	}
	var order []string
	queue := []string{}
	for p, d := range inDeg {
		if d == 0 {
			queue = append(queue, p)
		}
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		order = append(order, cur)
		for _, next := range adj[cur] {
			inDeg[next]--
			if inDeg[next] == 0 {
				queue = append(queue, next)
			}
		}
	}

	return &graph.Graph{Units: units, Order: order}, root
}

func TestFilterGraph_ReturnsEntireGraph_WhenNoFilter(t *testing.T) {
	// A → B → C
	g, _ := buildGraph(t, [][2]string{{"b", "a"}, {"c", "b"}})
	// No filter — FilterGraph not called by the orchestrator in this case,
	// but sanity-check with the deepest node returns all three.
	sub, err := FilterGraph(g, g.Order[len(g.Order)-1])
	if err != nil {
		t.Fatal(err)
	}
	if len(sub.Units) != 3 {
		t.Errorf("expected 3 units, got %d", len(sub.Units))
	}
}

func TestFilterGraph_IsolatedTarget(t *testing.T) {
	// Only one node, no edges.
	g, root := buildGraph(t, nil, "solo")
	sub, err := FilterGraph(g, filepath.Join(root, "solo"))
	if err != nil {
		t.Fatal(err)
	}
	if len(sub.Units) != 1 {
		t.Errorf("expected 1 unit, got %d", len(sub.Units))
	}
	if len(sub.Order) != 1 {
		t.Errorf("expected order length 1, got %d", len(sub.Order))
	}
}

func TestFilterGraph_TargetWithTwoDeps(t *testing.T) {
	// Diamond: D depends on B and C, both depend on A.
	// Filter for D → should return A, B, C, D.
	g, root := buildGraph(t, [][2]string{
		{"b", "a"}, {"c", "a"}, {"d", "b"}, {"d", "c"},
	})
	sub, err := FilterGraph(g, filepath.Join(root, "d"))
	if err != nil {
		t.Fatal(err)
	}
	if len(sub.Units) != 4 {
		t.Errorf("expected 4 units, got %d", len(sub.Units))
	}
}

func TestFilterGraph_ExcludesUnrelatedUnits(t *testing.T) {
	// A → B, C (standalone, no relation to A or B)
	g, root := buildGraph(t, [][2]string{{"b", "a"}}, "c")
	sub, err := FilterGraph(g, filepath.Join(root, "b"))
	if err != nil {
		t.Fatal(err)
	}
	if len(sub.Units) != 2 {
		t.Errorf("expected 2 units (a+b), got %d", len(sub.Units))
	}
	for p := range sub.Units {
		if filepath.Base(p) == "c" {
			t.Error("unrelated unit 'c' should not be in filtered graph")
		}
	}
}

func TestFilterGraph_PreservesTopologicalOrder(t *testing.T) {
	// C depends on B, B depends on A.
	g, root := buildGraph(t, [][2]string{{"b", "a"}, {"c", "b"}})
	sub, err := FilterGraph(g, filepath.Join(root, "c"))
	if err != nil {
		t.Fatal(err)
	}
	if len(sub.Order) != 3 {
		t.Fatalf("expected 3 in order, got %d", len(sub.Order))
	}
	pos := map[string]int{}
	for i, p := range sub.Order {
		pos[filepath.Base(p)] = i
	}
	if pos["a"] >= pos["b"] || pos["b"] >= pos["c"] {
		t.Errorf("order not topological: %v", sub.Order)
	}
}

func TestFilterGraph_UnknownTarget(t *testing.T) {
	g, _ := buildGraph(t, nil, "a")
	_, err := FilterGraph(g, "/nonexistent/path")
	if err == nil {
		t.Fatal("expected error for unknown target unit")
	}
}
