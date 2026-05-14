package graph

import (
	"context"
	"path/filepath"
	"testing"
)

func TestParseRenderedJSON_FullConfig(t *testing.T) {
	unitDir := "/project/live/prod/app"
	data := []byte(`{
		"dependency": {
			"vnet": {
				"config_path": "../vnet",
				"mock_outputs": {
					"vnet_id": "/subscriptions/fake/vnet",
					"subnet_ids": ["subnet-a", "subnet-b"]
				}
			},
			"storage": {
				"config_path": "../storage",
				"mock_outputs": {}
			}
		}
	}`)

	cfg, err := parseRenderedJSON(data, unitDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.Dependencies) != 2 {
		t.Fatalf("expected 2 dependencies, got %d", len(cfg.Dependencies))
	}

	vnet, ok := cfg.Dependencies["vnet"]
	if !ok {
		t.Fatal("expected dependency 'vnet'")
	}
	wantPath := filepath.Clean("/project/live/prod/vnet")
	if vnet.ConfigPath != wantPath {
		t.Errorf("vnet.ConfigPath = %q, want %q", vnet.ConfigPath, wantPath)
	}
	if vnet.MockOutputs["vnet_id"] != "/subscriptions/fake/vnet" {
		t.Errorf("vnet_id mock = %v", vnet.MockOutputs["vnet_id"])
	}

	storage := cfg.Dependencies["storage"]
	if len(storage.MockOutputs) != 0 {
		t.Errorf("storage should have empty mock_outputs, got %v", storage.MockOutputs)
	}
}

func TestParseRenderedJSON_AbsoluteConfigPath(t *testing.T) {
	// config_path that is already absolute should not be re-joined with unitDir.
	unitDir := "/project/live/prod/app"
	data := []byte(`{
		"dependency": {
			"shared": {
				"config_path": "/infra/shared/networking",
				"mock_outputs": null
			}
		}
	}`)

	cfg, err := parseRenderedJSON(data, unitDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dep := cfg.Dependencies["shared"]
	if dep.ConfigPath != "/infra/shared/networking" {
		t.Errorf("expected absolute path to be preserved, got %q", dep.ConfigPath)
	}
	if dep.MockOutputs == nil {
		t.Error("MockOutputs should never be nil after parsing")
	}
}

func TestParseRenderedJSON_NoDependencies(t *testing.T) {
	data := []byte(`{"inputs": {"foo": "bar"}}`)
	cfg, err := parseRenderedJSON(data, "/some/unit")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Dependencies) != 0 {
		t.Errorf("expected no dependencies, got %v", cfg.Dependencies)
	}
}

func TestParseRenderedJSON_InvalidJSON(t *testing.T) {
	_, err := parseRenderedJSON([]byte(`not json`), "/unit")
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestParseRenderedJSON_RelativePathTraversal(t *testing.T) {
	// Ensure ../../ style paths are resolved cleanly.
	unitDir := "/project/live/prod/app"
	data := []byte(`{
		"dependency": {
			"base": {
				"config_path": "../../base/network"
			}
		}
	}`)

	cfg, err := parseRenderedJSON(data, unitDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := filepath.Clean("/project/live/base/network")
	if cfg.Dependencies["base"].ConfigPath != want {
		t.Errorf("got %q, want %q", cfg.Dependencies["base"].ConfigPath, want)
	}
}

func TestConfigCache_HitsOnSecondCall(t *testing.T) {
	// loadConfig is called twice for the same path; the second call must not
	// invoke renderJSON again (we detect this by tracking call count via a sentinel).
	hitCount := 0
	original := renderJSONFunc
	defer func() { renderJSONFunc = original }()

	renderJSONFunc = func(_ context.Context, _ string) ([]byte, error) {
		hitCount++
		return []byte(`{}`), nil
	}

	cache := newConfigCache()
	ctx := context.Background()

	if _, err := loadConfig(ctx, "/unit/a", cache); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(ctx, "/unit/a", cache); err != nil {
		t.Fatal(err)
	}

	if hitCount != 1 {
		t.Errorf("renderJSONFunc called %d times, want 1 (one miss, one cache hit)", hitCount)
	}
}
