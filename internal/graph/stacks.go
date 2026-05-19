package graph

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DiscoverStacks walks workingDir recursively and returns the absolute paths of
// all directories that contain a terragrunt.stack.hcl file. The workingDir
// itself is included when it contains a stack file. Directories that are
// Terragrunt cache dirs (.terragrunt-cache, .terraform) are skipped.
//
// Results are sorted by depth (shallowest first) so callers can process parent
// stacks before child stacks when ordering matters.
func DiscoverStacks(workingDir string) ([]string, error) {
	abs, err := filepath.Abs(workingDir)
	if err != nil {
		return nil, err
	}

	var stacks []string
	err = filepath.WalkDir(abs, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		// Skip generated/cache directories that are never user-authored stacks.
		name := d.Name()
		if name == ".terragrunt-cache" || name == ".terraform" || strings.HasPrefix(name, ".tg-simulate-") {
			return filepath.SkipDir
		}

		stackFile := filepath.Join(path, "terragrunt.stack.hcl")
		if _, err := os.Stat(stackFile); err == nil {
			stacks = append(stacks, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Sort by depth (number of path separators) so parent stacks come first.
	sort.Slice(stacks, func(i, j int) bool {
		di := depthOf(stacks[i])
		dj := depthOf(stacks[j])
		if di != dj {
			return di < dj
		}
		return stacks[i] < stacks[j]
	})

	return stacks, nil
}

func depthOf(path string) int {
	count := 0
	for _, c := range path {
		if c == filepath.Separator {
			count++
		}
	}
	return count
}
