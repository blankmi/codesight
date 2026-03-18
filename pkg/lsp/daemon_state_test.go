package lsp

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestStateReadWriteRoundTripWithSocketPath(t *testing.T) {
	stateDir := t.TempDir()
	stateKey := "roundtrip"
	statePath := filepath.Join(stateDir, stateKey+lifecycleStateFileExtension)
	socketPath := filepath.Join(stateDir, stateKey+lifecycleSocketFileExtension)

	original := lifecycleState{
		WorkspaceRoot:    "/repo",
		Language:         "go",
		StateKey:         stateKey,
		SocketPath:       socketPath,
		PID:              1234,
		Binary:           "gopls",
		Args:             []string{"serve"},
		StartedUnixNano:  100,
		LastUsedUnixNano: 200,
		JavaGradleBuildBaseline: &JavaGradleBuildBaseline{
			Fingerprint: "baseline-fingerprint",
			Files: []JavaGradleBuildFile{
				{
					Path:            "build.gradle",
					Exists:          true,
					ModTimeUnixNano: 5,
					SizeBytes:       10,
					ContentSHA256:   "abc123",
				},
			},
		},
	}

	if err := writeStateFile(statePath, original); err != nil {
		t.Fatalf("writeStateFile returned error: %v", err)
	}

	loaded, err := readStateFile(statePath)
	if err != nil {
		t.Fatalf("readStateFile returned error: %v", err)
	}

	if loaded.StateKey != original.StateKey {
		t.Fatalf("loaded state key = %q, want %q", loaded.StateKey, original.StateKey)
	}
	if loaded.SocketPath != original.SocketPath {
		t.Fatalf("loaded socket path = %q, want %q", loaded.SocketPath, original.SocketPath)
	}
	if loaded.PID != original.PID {
		t.Fatalf("loaded pid = %d, want %d", loaded.PID, original.PID)
	}
	if loaded.JavaGradleBuildBaseline == nil {
		t.Fatal("loaded java gradle baseline = nil, want non-nil baseline")
	}
	if loaded.JavaGradleBuildBaseline.Fingerprint != original.JavaGradleBuildBaseline.Fingerprint {
		t.Fatalf("loaded java baseline fingerprint = %q, want %q", loaded.JavaGradleBuildBaseline.Fingerprint, original.JavaGradleBuildBaseline.Fingerprint)
	}
	if len(loaded.JavaGradleBuildBaseline.Files) != 1 || loaded.JavaGradleBuildBaseline.Files[0].Path != "build.gradle" {
		t.Fatalf("loaded java baseline files = %#v, want one build.gradle entry", loaded.JavaGradleBuildBaseline.Files)
	}
}

func TestStateReadLegacyPayloadWithoutSocketPath(t *testing.T) {
	stateDir := t.TempDir()
	stateKey := "legacy"
	statePath := filepath.Join(stateDir, stateKey+lifecycleStateFileExtension)

	legacyPayload := []byte(`{"workspace_root":"/legacy/repo","language":"go","pid":88,"binary":"gopls","args":["serve"],"started_unix_nano":1,"last_used_unix_nano":2}`)
	if err := os.WriteFile(statePath, legacyPayload, 0o600); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	loaded, err := readStateFile(statePath)
	if err != nil {
		t.Fatalf("readStateFile returned error: %v", err)
	}

	wantSocketPath := filepath.Join(stateDir, stateKey+lifecycleSocketFileExtension)
	if loaded.StateKey != stateKey {
		t.Fatalf("loaded state key = %q, want %q", loaded.StateKey, stateKey)
	}
	if loaded.SocketPath != wantSocketPath {
		t.Fatalf("loaded socket path = %q, want %q", loaded.SocketPath, wantSocketPath)
	}
}

func TestStateReadIgnoresUntrustedSocketPathPayload(t *testing.T) {
	testCases := []struct {
		name       string
		socketPath string
	}{
		{name: "absolute mismatch", socketPath: "/tmp/untrusted.sock"},
		{name: "relative path", socketPath: "../escape.sock"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			stateDir := t.TempDir()
			stateKey := "derived-key"
			statePath := filepath.Join(stateDir, stateKey+lifecycleStateFileExtension)

			payload := fmt.Sprintf(
				`{"workspace_root":"/repo","language":"go","state_key":"tampered","socket_path":%q,"pid":1,"binary":"gopls","args":["serve"],"started_unix_nano":1,"last_used_unix_nano":2}`,
				tc.socketPath,
			)
			if err := os.WriteFile(statePath, []byte(payload), 0o600); err != nil {
				t.Fatalf("os.WriteFile returned error: %v", err)
			}

			loaded, err := readStateFile(statePath)
			if err != nil {
				t.Fatalf("readStateFile returned error: %v", err)
			}

			wantSocketPath := filepath.Join(stateDir, stateKey+lifecycleSocketFileExtension)
			if loaded.StateKey != stateKey {
				t.Fatalf("loaded state key = %q, want %q", loaded.StateKey, stateKey)
			}
			if loaded.SocketPath != wantSocketPath {
				t.Fatalf("loaded socket path = %q, want %q", loaded.SocketPath, wantSocketPath)
			}
		})
	}
}

func TestStateReadCorruptJSONReturnsError(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "corrupt"+lifecycleStateFileExtension)
	if err := os.WriteFile(statePath, []byte("{corrupt"), 0o600); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	_, err := readStateFile(statePath)
	if err == nil {
		t.Fatal("readStateFile should return an error for corrupt JSON")
	}
}

func TestStateWriteFilePermissionsAreUserOnly(t *testing.T) {
	stateDir := t.TempDir()
	stateKey := "perms"
	statePath := filepath.Join(stateDir, stateKey+lifecycleStateFileExtension)

	if err := writeStateFile(statePath, lifecycleState{
		WorkspaceRoot: "/repo",
		Language:      "go",
		StateKey:      stateKey,
	}); err != nil {
		t.Fatalf("writeStateFile returned error: %v", err)
	}

	info, err := os.Stat(statePath)
	if err != nil {
		t.Fatalf("os.Stat returned error: %v", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("state file permissions = %#o, expected user-only permissions", info.Mode().Perm())
	}

	loaded, err := readStateFile(statePath)
	if err != nil {
		t.Fatalf("readStateFile returned error: %v", err)
	}
	wantSocketPath := filepath.Join(stateDir, stateKey+lifecycleSocketFileExtension)
	if loaded.SocketPath != wantSocketPath {
		t.Fatalf("loaded socket path = %q, want %q", loaded.SocketPath, wantSocketPath)
	}
}

func TestJavaGradleBuildBaselineReadWriteRoundTrip(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv(StateDirEnvVar, stateDir)

	workspace := t.TempDir()
	baseline := JavaGradleBuildBaseline{
		Fingerprint: "fingerprint",
		Files: []JavaGradleBuildFile{
			{
				Path:            "settings.gradle",
				Exists:          true,
				ModTimeUnixNano: 123,
				SizeBytes:       456,
				ContentSHA256:   "hash",
			},
		},
	}

	if err := WriteJavaGradleBuildBaseline(workspace, baseline); err != nil {
		t.Fatalf("WriteJavaGradleBuildBaseline returned error: %v", err)
	}

	loaded, err := ReadJavaGradleBuildBaseline(workspace)
	if err != nil {
		t.Fatalf("ReadJavaGradleBuildBaseline returned error: %v", err)
	}
	if loaded.Fingerprint != baseline.Fingerprint {
		t.Fatalf("loaded baseline fingerprint = %q, want %q", loaded.Fingerprint, baseline.Fingerprint)
	}
	if len(loaded.Files) != 1 || loaded.Files[0].Path != baseline.Files[0].Path {
		t.Fatalf("loaded baseline files = %#v, want %#v", loaded.Files, baseline.Files)
	}
}

func TestWriteJavaGradleBuildBaselineUpdatesActiveLifecycleState(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv(StateDirEnvVar, stateDir)

	workspace := t.TempDir()
	workspaceAbs, err := filepath.Abs(workspace)
	if err != nil {
		t.Fatalf("filepath.Abs returned error: %v", err)
	}
	stateKey := StateKey(workspaceAbs, "java")

	stateRoot := filepath.Join(stateDir, lspStateSubdir)
	if err := os.MkdirAll(stateRoot, 0o700); err != nil {
		t.Fatalf("os.MkdirAll returned error: %v", err)
	}

	statePath := filepath.Join(stateRoot, stateKey+lifecycleStateFileExtension)
	if err := writeStateFile(statePath, lifecycleState{
		WorkspaceRoot: workspaceAbs,
		Language:      "java",
		StateKey:      stateKey,
		PID:           100,
		Binary:        "jdtls",
	}); err != nil {
		t.Fatalf("writeStateFile returned error: %v", err)
	}

	baseline := JavaGradleBuildBaseline{
		Fingerprint: "fingerprint-from-writer",
	}
	if err := WriteJavaGradleBuildBaseline(workspace, baseline); err != nil {
		t.Fatalf("WriteJavaGradleBuildBaseline returned error: %v", err)
	}

	loadedState, err := readStateFile(statePath)
	if err != nil {
		t.Fatalf("readStateFile returned error: %v", err)
	}
	if loadedState.JavaGradleBuildBaseline == nil {
		t.Fatal("lifecycle state java baseline = nil, want non-nil baseline")
	}
	if loadedState.JavaGradleBuildBaseline.Fingerprint != baseline.Fingerprint {
		t.Fatalf("lifecycle state baseline fingerprint = %q, want %q", loadedState.JavaGradleBuildBaseline.Fingerprint, baseline.Fingerprint)
	}
}

func TestWriteJavaGradleBuildBaselineRequiresFingerprint(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv(StateDirEnvVar, stateDir)

	err := WriteJavaGradleBuildBaseline(t.TempDir(), JavaGradleBuildBaseline{})
	if err == nil {
		t.Fatal("WriteJavaGradleBuildBaseline error = nil, want fingerprint validation error")
	}
	if err.Error() != "java gradle build baseline fingerprint is required" {
		t.Fatalf("WriteJavaGradleBuildBaseline error = %q, want fingerprint validation error", err.Error())
	}
}
