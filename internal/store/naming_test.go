package store

import "testing"

// TestDeriveName covers name derivation from files and URLs, the addressing key
// every later task uses. It lives next to the implementation because the indexer
// and the query layer must share one derivation.
func TestDeriveName(t *testing.T) {
	cases := []struct {
		source string
		want   string
	}{
		{"testdata/petstore.json", "petstore"},
		{"/tmp/specs/billing-api.yaml", "billing-api"},
		{`C:\work\specs\payments.yml`, "payments"},
		{"https://example.com/openapi/petstore.json", "petstore"},
		{"https://example.com/openapi/petstore.json?version=2", "petstore"},
		{"https://example.com/", "example.com"},
		{"https://example.com", "example.com"},
		{"", "spec"},
		// Two sources under the same base name derive the same name; that is
		// exactly the collision the indexer must refuse instead of overwriting.
		{"/specs/v1/api.json", "api"},
		{"/specs/v2/api.json", "api"},
	}
	for _, tc := range cases {
		if got := DeriveName(tc.source); got != tc.want {
			t.Errorf("DeriveName(%q) = %q, want %q", tc.source, got, tc.want)
		}
	}
}

// TestIsURL covers the check the indexer uses to decide whether a source needs
// an HTTP read.
func TestIsURL(t *testing.T) {
	cases := []struct {
		source string
		want   bool
	}{
		{"https://example.com/openapi.json", true},
		{"http://example.com/openapi.json", true},
		{"  https://example.com/openapi.json  ", true},
		{"testdata/petstore.json", false},
		{`C:\work\specs\payments.yml`, false},
		{"fixture://petstore", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := IsURL(tc.source); got != tc.want {
			t.Errorf("IsURL(%q) = %v, want %v", tc.source, got, tc.want)
		}
	}
}
