package simulator

import (
	"crypto/rand"
	"fmt"
	"strings"
)

// TypeHint carries context that guides synthetic value generation.
type TypeHint struct {
	// OutputName is the Terraform output variable name (e.g. "vnet_id", "subnet_ids").
	OutputName string
	// ExistingMock is the current mock_outputs value from a downstream unit's
	// terragrunt.hcl, if present. It provides the strongest shape hint.
	ExistingMock interface{}
}

// SyntheticGenerator produces a plausible-looking value for a plan output that
// is (known after apply). Every value it returns must be visibly tagged so it
// can be distinguished from a real value at a glance.
type SyntheticGenerator interface {
	Generate(hint TypeHint) interface{}
}

// Default is the generator used throughout tg-simulate. It combines mock-shape
// inference with name heuristics.
var Default SyntheticGenerator = &HeuristicGenerator{}

// HeuristicGenerator is the built-in generator. It first tries to mirror the
// shape of an existing mock value, then falls back to name-pattern heuristics.
type HeuristicGenerator struct{}

func (g *HeuristicGenerator) Generate(hint TypeHint) interface{} {
	if hint.ExistingMock != nil {
		if v := generateFromMockShape(hint.OutputName, hint.ExistingMock); v != nil {
			return v
		}
	}
	return generateFromName(hint.OutputName)
}

const maxMockDepth = 10

// generateFromMockShape infers a synthetic value by examining the shape and
// content of an existing mock value.
func generateFromMockShape(name string, mock interface{}) interface{} {
	return generateFromMockShapeDepth(name, mock, 0)
}

func generateFromMockShapeDepth(name string, mock interface{}, depth int) interface{} {
	switch v := mock.(type) {
	case string:
		return simulateLikeString(name, v)
	case []interface{}:
		return simulateLikeList(name, v)
	case map[string]interface{}:
		if depth >= maxMockDepth {
			return "sim-" + name
		}
		return simulateLikeMapDepth(name, v, depth+1)
	}
	return nil
}

// simulateLikeString produces a synthetic string that mirrors the pattern of an existing value.
func simulateLikeString(name, existing string) string {
	switch {
	case strings.HasPrefix(existing, "/subscriptions/"):
		return genAzureID(name, existing)
	case strings.HasPrefix(existing, "arn:"):
		return genAWSARN(name, existing)
	case isUUIDLike(existing):
		return genUUID()
	case isIPLike(existing):
		return genIP()
	case strings.HasPrefix(existing, "https://") || strings.HasPrefix(existing, "http://"):
		return genURL(name)
	}
	return "sim-" + name
}

// simulateLikeList generates a list of the same length (min 1, max capped at 3)
// with synthetic values inferred from the first element.
func simulateLikeList(name string, existing []interface{}) []interface{} {
	count := len(existing)
	if count == 0 {
		count = 1
	}
	if count > 3 {
		count = 3
	}
	out := make([]interface{}, count)
	for i := range out {
		indexed := fmt.Sprintf("%s-%d", name, i+1)
		if len(existing) > 0 {
			if s, ok := existing[0].(string); ok {
				out[i] = simulateLikeString(indexed, s)
				continue
			}
		}
		out[i] = "sim-" + indexed
	}
	return out
}

func simulateLikeMapDepth(name string, existing map[string]interface{}, depth int) map[string]interface{} {
	out := make(map[string]interface{}, len(existing))
	for k, v := range existing {
		out[k] = generateFromMockShapeDepth(k, v, depth)
		if out[k] == nil {
			out[k] = generateFromName(k)
		}
	}
	return out
}

// generateFromName falls back to output name pattern matching.
func generateFromName(name string) interface{} {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, "_arn"):
		return genAWSARN(name, "")
	case strings.HasSuffix(lower, "_ids"):
		return []interface{}{"sim-" + genUUID(), "sim-" + genUUID()}
	case strings.HasSuffix(lower, "_id"):
		return genUUID()
	case strings.HasSuffix(lower, "_ip") ||
		strings.HasSuffix(lower, "_address") ||
		strings.HasSuffix(lower, "_cidr"):
		return genIP()
	case strings.HasSuffix(lower, "_url") ||
		strings.HasSuffix(lower, "_endpoint") ||
		strings.HasSuffix(lower, "_uri"):
		return genURL(name)
	case strings.HasSuffix(lower, "_connection_string") ||
		strings.HasSuffix(lower, "_connection_str"):
		return genConnectionString(name)
	case strings.HasSuffix(lower, "_name") ||
		strings.HasSuffix(lower, "_key") ||
		strings.HasSuffix(lower, "_tag"):
		return "sim-" + name
	}
	return "sim-" + name
}

// --- provider-specific generators ---

const (
	simSubscription = "00000000-0000-0000-0000-000000000000"
	simRG           = "sim-rg"
	simAWSAccount   = "000000000000"
	simAWSRegion    = "us-east-1"
)

// genAzureID produces an Azure resource ID with sim- markers in the name segments.
// If existing is provided it tries to preserve the provider/type path.
func genAzureID(name, existing string) string {
	provider := "Microsoft.Resources/deployments"
	resourceName := "sim-" + name

	if existing != "" {
		// Extract provider path from existing: .../providers/{ns}/{type}/...
		if idx := strings.Index(existing, "/providers/"); idx >= 0 {
			rest := existing[idx+len("/providers/"):]
			parts := strings.SplitN(rest, "/", 3)
			if len(parts) >= 2 {
				provider = parts[0] + "/" + parts[1]
			}
		}
	}

	return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/%s/%s",
		simSubscription, simRG, provider, resourceName)
}

// genAWSARN produces an AWS ARN with sim- in the resource segment.
// If existing is provided it preserves partition, service, and region.
func genAWSARN(name, existing string) string {
	partition, service, region := "aws", "resource", simAWSRegion

	if existing != "" {
		// arn:{partition}:{service}:{region}:{account}:{resource}
		parts := strings.SplitN(existing, ":", 6)
		if len(parts) >= 4 {
			partition = parts[1]
			service = parts[2]
			if parts[3] != "" {
				region = parts[3]
			}
		}
	}

	return fmt.Sprintf("arn:%s:%s:%s:%s:sim-%s",
		partition, service, region, simAWSAccount, name)
}

func genUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant bits
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func genIP() string {
	// 192.0.2.0/24 is reserved for documentation (RFC 5737) — will never route.
	b := make([]byte, 1)
	_, _ = rand.Read(b)
	return fmt.Sprintf("192.0.2.%d", int(b[0]))
}

func genURL(name string) string {
	return fmt.Sprintf("https://sim-%s.example.com", strings.ReplaceAll(name, "_", "-"))
}

func genConnectionString(name string) string {
	return fmt.Sprintf("Server=sim-%s-server.example.com;Database=sim-db;User Id=sim-user;Password=sim-password;",
		strings.ReplaceAll(name, "_connection_string", ""))
}

// --- detection helpers ---

func isUUIDLike(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
			continue
		}
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

func isIPLike(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) != 4 {
		return false
	}
	for _, p := range parts {
		if len(p) == 0 || len(p) > 3 {
			return false
		}
		for _, c := range p {
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	return true
}
