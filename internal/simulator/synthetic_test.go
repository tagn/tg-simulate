package simulator

import (
	"strings"
	"testing"
)

// simTagged asserts v is a string and contains "sim" (the required visibility tag).
func simTagged(t *testing.T, label string, v interface{}) string {
	t.Helper()
	s, ok := v.(string)
	if !ok {
		t.Errorf("%s: expected string, got %T (%v)", label, v, v)
		return ""
	}
	if !strings.Contains(strings.ToLower(s), "sim") {
		t.Errorf("%s: value %q does not contain 'sim' tag", label, s)
	}
	return s
}

// --- name heuristic tests ---

func TestGenerateFromName_ARN(t *testing.T) {
	v := Default.Generate(TypeHint{OutputName: "lambda_arn"})
	s := simTagged(t, "lambda_arn", v)
	if !strings.HasPrefix(s, "arn:") {
		t.Errorf("expected ARN prefix, got %q", s)
	}
}

func TestGenerateFromName_IDs(t *testing.T) {
	v := Default.Generate(TypeHint{OutputName: "subnet_ids"})
	list, ok := v.([]interface{})
	if !ok {
		t.Fatalf("subnet_ids: expected []interface{}, got %T", v)
	}
	if len(list) < 1 {
		t.Error("expected at least one element in list")
	}
}

func TestGenerateFromName_ID(t *testing.T) {
	v := Default.Generate(TypeHint{OutputName: "resource_id"})
	s, ok := v.(string)
	if !ok {
		t.Fatalf("resource_id: expected string, got %T", v)
	}
	if !isUUIDLike(s) {
		t.Errorf("resource_id: expected UUID-like string, got %q", s)
	}
}

func TestGenerateFromName_IP(t *testing.T) {
	v := Default.Generate(TypeHint{OutputName: "private_ip"})
	s, ok := v.(string)
	if !ok {
		t.Fatalf("private_ip: expected string, got %T", v)
	}
	if !isIPLike(s) {
		t.Errorf("private_ip: expected IP-like string, got %q", s)
	}
	// RFC 5737 documentation range is the visibility signal for synthetic IPs.
	if !strings.HasPrefix(s, "192.0.2.") {
		t.Errorf("private_ip: expected RFC 5737 documentation range (192.0.2.x), got %q", s)
	}
}

func TestGenerateFromName_URL(t *testing.T) {
	v := Default.Generate(TypeHint{OutputName: "api_endpoint"})
	s := simTagged(t, "api_endpoint", v)
	if !strings.HasPrefix(s, "https://") {
		t.Errorf("expected https:// prefix, got %q", s)
	}
}

func TestGenerateFromName_ConnectionString(t *testing.T) {
	v := Default.Generate(TypeHint{OutputName: "db_connection_string"})
	s := simTagged(t, "db_connection_string", v)
	if !strings.Contains(s, "Server=") {
		t.Errorf("expected connection string format, got %q", s)
	}
}

func TestGenerateFromName_Name(t *testing.T) {
	v := Default.Generate(TypeHint{OutputName: "cluster_name"})
	simTagged(t, "cluster_name", v)
}

func TestGenerateFromName_Default(t *testing.T) {
	v := Default.Generate(TypeHint{OutputName: "some_unknown_output"})
	simTagged(t, "some_unknown_output", v)
}

// --- mock-shape inference tests ---

func TestGenerateFromMock_AzureID(t *testing.T) {
	mock := "/subscriptions/real-sub/resourceGroups/prod-rg/providers/Microsoft.Network/virtualNetworks/prod-vnet"
	v := Default.Generate(TypeHint{OutputName: "vnet_id", ExistingMock: mock})
	s := simTagged(t, "vnet_id", v)
	if !strings.HasPrefix(s, "/subscriptions/") {
		t.Errorf("expected Azure ID prefix, got %q", s)
	}
	if !strings.Contains(s, "Microsoft.Network") {
		t.Errorf("expected provider path preserved, got %q", s)
	}
}

func TestGenerateFromMock_AWSARN(t *testing.T) {
	mock := "arn:aws:lambda:eu-west-1:123456789012:function:prod-fn"
	v := Default.Generate(TypeHint{OutputName: "fn_arn", ExistingMock: mock})
	s := simTagged(t, "fn_arn", v)
	if !strings.HasPrefix(s, "arn:aws:lambda:eu-west-1:") {
		t.Errorf("expected ARN with preserved partition/service/region, got %q", s)
	}
}

func TestGenerateFromMock_UUID(t *testing.T) {
	mock := "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
	v := Default.Generate(TypeHint{OutputName: "tenant_id", ExistingMock: mock})
	s, ok := v.(string)
	if !ok {
		t.Fatalf("expected string, got %T", v)
	}
	if !isUUIDLike(s) {
		t.Errorf("expected UUID-like output, got %q", s)
	}
}

func TestGenerateFromMock_List(t *testing.T) {
	mock := []interface{}{"/subscriptions/real/resourceGroups/rg/providers/Microsoft.Network/subnets/subnet-1"}
	v := Default.Generate(TypeHint{OutputName: "subnet_ids", ExistingMock: mock})
	list, ok := v.([]interface{})
	if !ok {
		t.Fatalf("expected []interface{}, got %T", v)
	}
	if len(list) == 0 {
		t.Error("expected non-empty list")
	}
	first := simTagged(t, "subnet_ids[0]", list[0])
	if !strings.HasPrefix(first, "/subscriptions/") {
		t.Errorf("list element should mirror mock shape, got %q", first)
	}
}

func TestGenerateFromMock_Map(t *testing.T) {
	mock := map[string]interface{}{
		"id":   "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
		"name": "prod-cluster",
	}
	v := Default.Generate(TypeHint{OutputName: "cluster", ExistingMock: mock})
	m, ok := v.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T", v)
	}
	if _, ok := m["id"]; !ok {
		t.Error("expected 'id' key in output map")
	}
	if _, ok := m["name"]; !ok {
		t.Error("expected 'name' key in output map")
	}
}

func TestGenerateFromMock_URL(t *testing.T) {
	mock := "https://prod-api.example.com"
	v := Default.Generate(TypeHint{OutputName: "api_url", ExistingMock: mock})
	s := simTagged(t, "api_url", v)
	if !strings.HasPrefix(s, "https://") {
		t.Errorf("expected https:// prefix, got %q", s)
	}
}

func TestGenerateFromMock_IP(t *testing.T) {
	mock := "10.0.1.5"
	v := Default.Generate(TypeHint{OutputName: "private_ip", ExistingMock: mock})
	s, ok := v.(string)
	if !ok {
		t.Fatalf("expected string, got %T", v)
	}
	if !isIPLike(s) {
		t.Errorf("expected IP-like output, got %q", s)
	}
	// RFC 5737 documentation range is the visibility signal for synthetic IPs.
	if !strings.HasPrefix(s, "192.0.2.") {
		t.Errorf("expected RFC 5737 documentation range (192.0.2.x), got %q", s)
	}
}

// --- helpers ---

func TestIsUUIDLike(t *testing.T) {
	cases := []struct{ s string; want bool }{
		{"a1b2c3d4-e5f6-7890-abcd-ef1234567890", true},
		{"not-a-uuid", false},
		{"00000000-0000-0000-0000-000000000000", true},
		{"00000000-0000-0000-0000-00000000000g", false}, // 'g' not hex
	}
	for _, c := range cases {
		if got := isUUIDLike(c.s); got != c.want {
			t.Errorf("isUUIDLike(%q) = %v, want %v", c.s, got, c.want)
		}
	}
}

func TestIsIPLike(t *testing.T) {
	cases := []struct{ s string; want bool }{
		{"192.168.1.1", true},
		{"10.0.0.1", true},
		{"not-an-ip", false},
		{"192.168.1", false},
		{"192.168.1.1.1", false},
	}
	for _, c := range cases {
		if got := isIPLike(c.s); got != c.want {
			t.Errorf("isIPLike(%q) = %v, want %v", c.s, got, c.want)
		}
	}
}

func TestGenUUID_Format(t *testing.T) {
	for i := 0; i < 10; i++ {
		u := genUUID()
		if !isUUIDLike(u) {
			t.Errorf("genUUID() produced invalid UUID: %q", u)
		}
	}
}

func TestGenIP_DocumentationRange(t *testing.T) {
	for i := 0; i < 20; i++ {
		ip := genIP()
		if !strings.HasPrefix(ip, "192.0.2.") {
			t.Errorf("genIP() = %q, want 192.0.2.x (RFC 5737 documentation range)", ip)
		}
	}
}
