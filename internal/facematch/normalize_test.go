package facematch

import "testing"

func TestRemoveDiacritics(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Honza", "Honza"},
		{"Jiří", "Jiri"},
		{"café", "cafe"},
		{"naïve", "naive"},
		{"hello", "hello"},
		{"Žluťoučký kůň", "Zlutoucky kun"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := RemoveDiacritics(tt.input)
			if result != tt.expected {
				t.Errorf("RemoveDiacritics(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestNormalizePersonName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Jan Novák", "jan novak"},
		{"jan-novak", "jan novak"},
		{"JOHN DOE", "john doe"},
		{"jan-novák", "jan novak"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := NormalizePersonName(tt.input)
			if result != tt.expected {
				t.Errorf("NormalizePersonName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
