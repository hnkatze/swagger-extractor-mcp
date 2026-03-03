package types

import "testing"

func TestToolError_WithDetails(t *testing.T) {
	err := &ToolError{
		Code:    "INVALID_URL",
		Message: "bad url",
		Details: "missing scheme",
	}
	expected := "INVALID_URL: bad url (missing scheme)"
	if got := err.Error(); got != expected {
		t.Errorf("Error() = %q, want %q", got, expected)
	}
}

func TestToolError_WithoutDetails(t *testing.T) {
	err := &ToolError{
		Code:    "PARSE_ERROR",
		Message: "invalid json",
		Details: "",
	}
	expected := "PARSE_ERROR: invalid json"
	if got := err.Error(); got != expected {
		t.Errorf("Error() = %q, want %q", got, expected)
	}
}

func TestToolError_ImplementsError(t *testing.T) {
	var _ error = (*ToolError)(nil)
}

func TestErrorConstants(t *testing.T) {
	tests := []struct {
		name     string
		got      string
		expected string
	}{
		{"ErrInvalidURL", ErrInvalidURL, "INVALID_URL"},
		{"ErrParseError", ErrParseError, "PARSE_ERROR"},
		{"ErrNetworkError", ErrNetworkError, "NETWORK_ERROR"},
		{"ErrEndpointNotFound", ErrEndpointNotFound, "ENDPOINT_NOT_FOUND"},
		{"ErrInternalError", ErrInternalError, "INTERNAL_ERROR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.expected)
			}
		})
	}
}

func TestOutputFormat_Values(t *testing.T) {
	if FormatJSON != "json" {
		t.Errorf("FormatJSON = %q, want %q", FormatJSON, "json")
	}
	if FormatTOON != "toon" {
		t.Errorf("FormatTOON = %q, want %q", FormatTOON, "toon")
	}
}
