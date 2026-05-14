package graph

import (
	"os"
	"path/filepath"
	"testing"
)

// buildTempUnits creates stub terragrunt.hcl files for each path under a temp dir
// and returns the canonical temp dir as the working directory (symlinks resolved
// so it matches the paths produced by resolvePath + EvalSymlinks).
func buildTempUnits(t *testing.T, paths []string) string {
	t.Helper()
	root := t.TempDir()
	// Resolve symlinks so test-constructed paths match what resolvePath() returns.
	if canonical, err := filepath.EvalSymlinks(root); err == nil {
		root = canonical
	}
	for _, p := range paths {
		dir := filepath.Join(root, p)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "terragrunt.hcl"), []byte("# stub"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestParseDOT_SimpleChain(t *testing.T) {
	// C -> B -> A  (C depends on B, B depends on A)
	root := buildTempUnits(t, []string{"a", "b", "c"})
	dot := []byte(`digraph {
		"a" ;
		"b" ;
		"c" ;
		"b" -> "a" ;
		"c" -> "b" ;
	}`)

	g, err := parseDOT(dot, root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(g.Units) != 3 {
		t.Fatalf("expected 3 units, got %d", len(g.Units))
	}
	if len(g.Order) != 3 {
		t.Fatalf("expected order length 3, got %d", len(g.Order))
	}

	aPath := filepath.Join(root, "a")
	bPath := filepath.Join(root, "b")
	cPath := filepath.Join(root, "c")

	// a must come before b, b before c
	pos := make(map[string]int)
	for i, p := range g.Order {
		pos[p] = i
	}
	if pos[aPath] >= pos[bPath] {
		t.Errorf("expected a before b in order, got %v", g.Order)
	}
	if pos[bPath] >= pos[cPath] {
		t.Errorf("expected b before c in order, got %v", g.Order)
	}

	// Check dependency/dependent edges
	if len(g.Units[bPath].Dependencies) != 1 || g.Units[bPath].Dependencies[0] != aPath {
		t.Errorf("b.Dependencies: expected [%s], got %v", aPath, g.Units[bPath].Dependencies)
	}
	if len(g.Units[aPath].Dependents) != 1 || g.Units[aPath].Dependents[0] != bPath {
		t.Errorf("a.Dependents: expected [%s], got %v", bPath, g.Units[aPath].Dependents)
	}
}

func TestParseDOT_Diamond(t *testing.T) {
	// Diamond: A is root, B and C depend on A, D depends on B and C.
	// DOT: "b" -> "a" ; "c" -> "a" ; "d" -> "b" ; "d" -> "c"
	root := buildTempUnits(t, []string{"a", "b", "c", "d"})
	dot := []byte(`digraph {
		"a" ;
		"b" ;
		"c" ;
		"d" ;
		"b" -> "a" ;
		"c" -> "a" ;
		"d" -> "b" ;
		"d" -> "c" ;
	}`)

	g, err := parseDOT(dot, root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pos := make(map[string]int)
	for i, p := range g.Order {
		pos[p] = i
	}

	aPath := filepath.Join(root, "a")
	bPath := filepath.Join(root, "b")
	cPath := filepath.Join(root, "c")
	dPath := filepath.Join(root, "d")

	if pos[aPath] >= pos[bPath] {
		t.Errorf("expected a before b")
	}
	if pos[aPath] >= pos[cPath] {
		t.Errorf("expected a before c")
	}
	if pos[bPath] >= pos[dPath] {
		t.Errorf("expected b before d")
	}
	if pos[cPath] >= pos[dPath] {
		t.Errorf("expected c before d")
	}

	if len(g.Units[dPath].Dependencies) != 2 {
		t.Errorf("d should have 2 dependencies, got %d", len(g.Units[dPath].Dependencies))
	}
}

func TestParseDOT_IsolatedUnit(t *testing.T) {
	// A unit with no edges should still appear in the graph.
	root := buildTempUnits(t, []string{"solo"})
	dot := []byte(`digraph { "solo" ; }`)

	g, err := parseDOT(dot, root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(g.Units) != 1 {
		t.Fatalf("expected 1 unit, got %d", len(g.Units))
	}
	if len(g.Order) != 1 {
		t.Fatalf("expected order length 1, got %d", len(g.Order))
	}
}

func TestParseDOT_MalformedDOT(t *testing.T) {
	root := t.TempDir()
	_, err := parseDOT([]byte(`this is not dot`), root)
	if err == nil {
		t.Fatal("expected error for malformed DOT, got nil")
	}
}

func TestTopoSort_Cycle(t *testing.T) {
	// A -> B -> C -> A
	units := map[string]*Unit{
		"a": {Path: "a", Dependencies: []string{"c"}, Dependents: []string{"b"}},
		"b": {Path: "b", Dependencies: []string{"a"}, Dependents: []string{"c"}},
		"c": {Path: "c", Dependencies: []string{"b"}, Dependents: []string{"a"}},
	}
	_, err := topoSort(units)
	if err == nil {
		t.Fatal("expected cycle error, got nil")
	}
	if !containsAll(err.Error(), []string{"a", "b", "c"}) {
		t.Errorf("cycle error should mention all members, got: %v", err)
	}
}

func TestTopoSort_Deterministic(t *testing.T) {
	// Two independent units should always appear in the same order.
	units := map[string]*Unit{
		"z": {Path: "z", Dependencies: []string{}, Dependents: []string{}},
		"a": {Path: "a", Dependencies: []string{}, Dependents: []string{}},
		"m": {Path: "m", Dependencies: []string{}, Dependents: []string{}},
	}
	order1, err := topoSort(units)
	if err != nil {
		t.Fatal(err)
	}
	order2, err := topoSort(units)
	if err != nil {
		t.Fatal(err)
	}
	for i := range order1 {
		if order1[i] != order2[i] {
			t.Errorf("non-deterministic order: run1=%v run2=%v", order1, order2)
		}
	}
}

func TestValidateUnits_MissingHCL(t *testing.T) {
	root := t.TempDir()
	// Create a unit directory but no terragrunt.hcl inside.
	dir := filepath.Join(root, "missing")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	units := map[string]*Unit{dir: {Path: dir}}
	if err := validateUnits(units); err == nil {
		t.Fatal("expected error for missing terragrunt.hcl, got nil")
	}
}

func TestUnquote(t *testing.T) {
	cases := []struct{ in, want string }{
		{`"hello"`, "hello"},
		{`hello`, "hello"},
		{`""`, ""},
		{`"a/b/c"`, "a/b/c"},
	}
	for _, c := range cases {
		if got := unquote(c.in); got != c.want {
			t.Errorf("unquote(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func containsAll(s string, subs []string) bool {
	for _, sub := range subs {
		found := false
		for i := 0; i <= len(s)-len(sub); i++ {
			if s[i:i+len(sub)] == sub {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
