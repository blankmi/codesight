package main

import (
	"os"
	"path/filepath"
	"testing"
)

// setTestHome sets HOME and USERPROFILE to an isolated temp directory
// and returns its path.
func setTestHome(t *testing.T) string {
	t.Helper()

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)

	return homeDir
}

// clearTestEnv unsets all CODESIGHT_* environment variables for the duration
// of the test and restores their original values on cleanup.
func clearTestEnv(t *testing.T) {
	t.Helper()

	envKeys := []string{
		"CODESIGHT_DB_TYPE",
		"CODESIGHT_DB_ADDRESS",
		"CODESIGHT_DB_TOKEN",
		"CODESIGHT_OLLAMA_HOST",
		"CODESIGHT_EMBEDDING_MODEL",
		"CODESIGHT_OLLAMA_MAX_INPUT_CHARS",
		"CODESIGHT_STATE_DIR",
		"CODESIGHT_GRADLE_JAVA_HOME",
	}

	previousValues := map[string]*string{}
	for _, key := range envKeys {
		if value, ok := os.LookupEnv(key); ok {
			copyValue := value
			previousValues[key] = &copyValue
		} else {
			previousValues[key] = nil
		}
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("Unsetenv(%q): %v", key, err)
		}
	}

	t.Cleanup(func() {
		for _, key := range envKeys {
			value := previousValues[key]
			if value == nil {
				_ = os.Unsetenv(key)
				continue
			}
			_ = os.Setenv(key, *value)
		}
	})
}

// writeTestFile creates a file at path with the given content, creating parent
// directories as needed.
func writeTestFile(t *testing.T, path string, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

// readTestFile reads the contents of a file, failing the test if it cannot.
func readTestFile(t *testing.T, path string) string {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	return string(content)
}
