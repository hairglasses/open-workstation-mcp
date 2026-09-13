package process

import (
	"reflect"
	"testing"
)

func TestIsolateProcessEnv(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "non-sensitive",
			input:    "PATH=/usr/bin",
			expected: "PATH=/usr/bin",
		},
		{
			name:     "sensitive api key",
			input:    "OPENAI_API_KEY=sk-12345",
			expected: "OPENAI_API_KEY=[REDACTED]",
		},
		{
			name:     "sensitive token",
			input:    "GITHUB_TOKEN=ghp_abc123",
			expected: "GITHUB_TOKEN=[REDACTED]",
		},
		{
			name:     "sensitive password",
			input:    "DB_PASSWORD=secret123",
			expected: "DB_PASSWORD=[REDACTED]",
		},
		{
			name:     "no equals",
			input:    "INVALID_ENV_VAR",
			expected: "INVALID_ENV_VAR",
		},
		{
			name:     "empty value",
			input:    "EMPTY_SECRET=",
			expected: "EMPTY_SECRET=[REDACTED]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsolateProcessEnv(tt.input); got != tt.expected {
				t.Errorf("IsolateProcessEnv(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestSafeCommandEnv(t *testing.T) {
	input := []string{
		"PATH=/usr/bin",
		"LANG=en_US.UTF-8",
		"OPENAI_API_KEY=sk-12345",
		"USER=root",
		"SECRET_TOKEN=abc",
		"TERM=xterm",
		"CUSTOM_VAR=yes",
	}

	expected := []string{
		"PATH=/usr/bin",
		"LANG=en_US.UTF-8",
		"USER=root",
		"TERM=xterm",
	}

	got := SafeCommandEnv(input)
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("SafeCommandEnv() = %v, want %v", got, expected)
	}
}
