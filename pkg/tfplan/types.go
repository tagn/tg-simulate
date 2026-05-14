// Package tfplan wraps hashicorp/terraform-json with helpers used across tg-simulate.
package tfplan

import (
	"encoding/json"
	"fmt"

	tfjson "github.com/hashicorp/terraform-json"
)

// Plan is an alias so internal packages only import this package.
type Plan = tfjson.Plan

// Change is an alias for a single resource or output change.
type Change = tfjson.Change

// Parse unmarshals raw plan JSON (from `terraform show -json`) into a Plan.
func Parse(data []byte) (*Plan, error) {
	var p tfjson.Plan
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parsing plan JSON: %w", err)
	}
	return &p, nil
}

// ChangeSummary holds the resource add/change/destroy counts for a single unit.
type ChangeSummary struct {
	Add     int
	Change  int
	Destroy int
}

// ResourceDetail holds the address and action for a single resource change.
type ResourceDetail struct {
	Address string
	Action  string // "add", "change", "destroy", "replace"
}

// Summarize counts resource changes in a plan, excluding no-ops and reads.
func Summarize(plan *Plan) ChangeSummary {
	var s ChangeSummary
	for _, rc := range plan.ResourceChanges {
		if rc.Change == nil {
			continue
		}
		c := rc.Change
		switch {
		case c.Actions.Create():
			s.Add++
		case c.Actions.Update():
			s.Change++
		case c.Actions.Delete() || c.Actions.Forget():
			s.Destroy++
		case c.Actions.Replace():
			// Replace counts as one destroy and one add.
			s.Add++
			s.Destroy++
		}
	}
	return s
}

// ResourceDetails returns per-resource change details for a plan, excluding no-ops and reads.
func ResourceDetails(plan *Plan) []ResourceDetail {
	var details []ResourceDetail
	for _, rc := range plan.ResourceChanges {
		if rc.Change == nil {
			continue
		}
		c := rc.Change
		var action string
		switch {
		case c.Actions.Create():
			action = "add"
		case c.Actions.Update():
			action = "change"
		case c.Actions.Delete() || c.Actions.Forget():
			action = "destroy"
		case c.Actions.Replace():
			action = "replace"
		default:
			continue
		}
		details = append(details, ResourceDetail{Address: rc.Address, Action: action})
	}
	return details
}

// IsOutputUnknown reports whether the after-value of an output change is unknown.
// AfterUnknown may be a bool (whole value unknown) or a nested map (partial).
// Returns true only when at least one field in the tree is actually unknown.
func IsOutputUnknown(c *Change) bool {
	if c.AfterUnknown == nil {
		return false
	}
	return containsUnknown(c.AfterUnknown)
}

// containsUnknown recursively checks whether v contains any true unknown marker.
func containsUnknown(v interface{}) bool {
	switch val := v.(type) {
	case bool:
		return val
	case map[string]interface{}:
		for _, child := range val {
			if containsUnknown(child) {
				return true
			}
		}
		return false
	}
	return false
}

// IsOutputSensitive reports whether the after-value of an output change is sensitive.
// AfterSensitive is true (bool) when the entire output is redacted.
func IsOutputSensitive(c *Change) bool {
	if c.AfterSensitive == nil {
		return false
	}
	b, ok := c.AfterSensitive.(bool)
	return ok && b
}
