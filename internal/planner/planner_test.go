package planner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/tagn/tg-simulate/internal/graph"
)

// stubUnit returns a minimal Unit with a real temp directory so os.MkdirTemp succeeds.
func stubUnit(t *testing.T) *graph.Unit {
	t.Helper()
	return &graph.Unit{Path: t.TempDir()}
}

func TestPlanUnit_NoChanges(t *testing.T) {
	orig1, orig2 := execTerragruntPlan, execTerraformShow
	defer func() { execTerragruntPlan, execTerraformShow = orig1, orig2 }()

	execTerragruntPlan = func(_ context.Context, _, _ string, _ PlanOptions) (bool, error) {
		return false, nil // exit 0
	}
	execTerraformShow = func(_ context.Context, _, _ string) ([]byte, error) {
		return []byte(`{"format_version":"1.0"}`), nil
	}

	result, err := PlanUnit(context.Background(), stubUnit(t), PlanOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.HasChanges {
		t.Error("expected HasChanges = false")
	}
	if string(result.PlanJSON) != `{"format_version":"1.0"}` {
		t.Errorf("unexpected PlanJSON: %s", result.PlanJSON)
	}
}

func TestPlanUnit_HasChanges(t *testing.T) {
	orig1, orig2 := execTerragruntPlan, execTerraformShow
	defer func() { execTerragruntPlan, execTerraformShow = orig1, orig2 }()

	execTerragruntPlan = func(_ context.Context, _, _ string, _ PlanOptions) (bool, error) {
		return true, nil // exit 2
	}
	execTerraformShow = func(_ context.Context, _, _ string) ([]byte, error) {
		return []byte(`{}`), nil
	}

	result, err := PlanUnit(context.Background(), stubUnit(t), PlanOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.HasChanges {
		t.Error("expected HasChanges = true")
	}
}

func TestPlanUnit_TerragruntError(t *testing.T) {
	orig1 := execTerragruntPlan
	defer func() { execTerragruntPlan = orig1 }()

	execTerragruntPlan = func(_ context.Context, _, _ string, _ PlanOptions) (bool, error) {
		return false, errors.New("terragrunt: provider error")
	}

	_, err := PlanUnit(context.Background(), stubUnit(t), PlanOptions{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestPlanUnit_TerraformShowError(t *testing.T) {
	orig1, orig2 := execTerragruntPlan, execTerraformShow
	defer func() { execTerragruntPlan, execTerraformShow = orig1, orig2 }()

	execTerragruntPlan = func(_ context.Context, _, _ string, _ PlanOptions) (bool, error) {
		return true, nil
	}
	execTerraformShow = func(_ context.Context, _, _ string) ([]byte, error) {
		return nil, errors.New("terraform: show failed")
	}

	_, err := PlanUnit(context.Background(), stubUnit(t), PlanOptions{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestPlanUnit_UnitPathPreserved(t *testing.T) {
	orig1, orig2 := execTerragruntPlan, execTerraformShow
	defer func() { execTerragruntPlan, execTerraformShow = orig1, orig2 }()

	unit := stubUnit(t)
	var capturedWorkDir string

	execTerragruntPlan = func(_ context.Context, workingDir, _ string, _ PlanOptions) (bool, error) {
		capturedWorkDir = workingDir
		return false, nil
	}
	execTerraformShow = func(_ context.Context, _, _ string) ([]byte, error) {
		return []byte(`{}`), nil
	}

	if _, err := PlanUnit(context.Background(), unit, PlanOptions{}); err != nil {
		t.Fatal(err)
	}
	if capturedWorkDir != unit.Path {
		t.Errorf("working dir = %q, want %q", capturedWorkDir, unit.Path)
	}
}

func TestPlanUnit_PlanFileInTempDir(t *testing.T) {
	orig1, orig2 := execTerragruntPlan, execTerraformShow
	defer func() { execTerragruntPlan, execTerraformShow = orig1, orig2 }()

	var capturedPlanFile string

	execTerragruntPlan = func(_ context.Context, _, planFile string, _ PlanOptions) (bool, error) {
		capturedPlanFile = planFile
		return false, nil
	}
	execTerraformShow = func(_ context.Context, _, _ string) ([]byte, error) {
		return []byte(`{}`), nil
	}

	if _, err := PlanUnit(context.Background(), stubUnit(t), PlanOptions{}); err != nil {
		t.Fatal(err)
	}

	if filepath.Base(capturedPlanFile) != "tfplan.binary" {
		t.Errorf("plan file name = %q, want tfplan.binary", filepath.Base(capturedPlanFile))
	}

	// Temp dir must be cleaned up after the call.
	dir := filepath.Dir(capturedPlanFile)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("temp dir %q should have been removed after PlanUnit returned", dir)
	}
}

func TestPlanUnit_OverlayPathForwarded(t *testing.T) {
	orig1, orig2 := execTerragruntPlan, execTerraformShow
	defer func() { execTerragruntPlan, execTerraformShow = orig1, orig2 }()

	var capturedOpts PlanOptions

	execTerragruntPlan = func(_ context.Context, _, _ string, opts PlanOptions) (bool, error) {
		capturedOpts = opts
		return false, nil
	}
	execTerraformShow = func(_ context.Context, _, _ string) ([]byte, error) {
		return []byte(`{}`), nil
	}

	opts := PlanOptions{OverlayPath: "/tmp/overlay/terragrunt.hcl", MergeStrategy: "no_merge"}
	if _, err := PlanUnit(context.Background(), stubUnit(t), opts); err != nil {
		t.Fatal(err)
	}
	if capturedOpts.OverlayPath != opts.OverlayPath {
		t.Errorf("OverlayPath not forwarded: got %q", capturedOpts.OverlayPath)
	}
}
