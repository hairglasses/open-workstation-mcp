package process

import (
	"strings"
)

// IsolateProcessEnv redacts sensitive values from a process environment variable string.
// If the variable contains a sensitive key, its value is replaced with [REDACTED].
// The input is expected to be in "KEY=VALUE" format.
func IsolateProcessEnv(envVar string) string {
	parts := strings.SplitN(envVar, "=", 2)
	if len(parts) != 2 {
		return envVar
	}
	key := parts[0]
	if IsSensitiveEnvKey(key) {
		return key + "=[REDACTED]"
	}
	return envVar
}

// IsSensitiveEnvKey returns true if the key is typically considered sensitive.
func IsSensitiveEnvKey(key string) bool {
	k := strings.ToLower(key)
	sensitiveTokens := []string{
		"secret",
		"token",
		"password",
		"passwd",
		"api_key",
		"apikey",
		"auth",
		"credential",
		"cert",
		"private",
		"session",
		"key",
	}
	for _, token := range sensitiveTokens {
		if strings.Contains(k, token) {
			return true
		}
	}
	return false
}

// SafeCommandEnv filters an environment slice (like from os.Environ()),
// returning only a safe subset for executing system subprocesses.
func SafeCommandEnv(env []string) []string {
	var safe []string
	prefixes := []string{"PATH=", "LANG=", "LC_", "USER=", "HOME=", "TERM="}
	for _, e := range env {
		for _, prefix := range prefixes {
			if strings.HasPrefix(e, prefix) {
				safe = append(safe, e)
				break
			}
		}
	}
	return safe
}
