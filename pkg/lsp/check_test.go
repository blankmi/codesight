package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type stubCheckClient struct {
	handler          func(json.RawMessage)
	diagnosticsByURI map[DocumentURI][]PublishDiagnosticsParams
	openedURIs       []DocumentURI
	closedURIs       []DocumentURI
}

func (s *stubCheckClient) Notify(_ context.Context, method string, params any) error {
	switch method {
	case MethodTextDocumentDidOpen:
		typed, ok := params.(DidOpenTextDocumentParams)
		if !ok {
			return fmt.Errorf("didOpen params type %T", params)
		}
		s.openedURIs = append(s.openedURIs, typed.TextDocument.URI)
		if s.handler != nil {
			for _, notification := range s.diagnosticsByURI[typed.TextDocument.URI] {
				raw, err := json.Marshal(notification)
				if err != nil {
					return err
				}
				s.handler(raw)
			}
		}
		return nil
	case MethodTextDocumentDidClose:
		typed, ok := params.(DidCloseTextDocumentParams)
		if !ok {
			return fmt.Errorf("didClose params type %T", params)
		}
		s.closedURIs = append(s.closedURIs, typed.TextDocument.URI)
		return nil
	default:
		return fmt.Errorf("unexpected method %q", method)
	}
}

func (s *stubCheckClient) SubscribeNotification(method string, handler func(json.RawMessage)) (func(), error) {
	if method != MethodTextDocumentPublishDiagnostics {
		return nil, fmt.Errorf("unexpected notification method %q", method)
	}
	s.handler = handler
	return func() {
		s.handler = nil
	}, nil
}

func TestCheckEngineFormatsDiagnosticsDeterministically(t *testing.T) {
	workspace := t.TempDir()
	alphaPath := filepath.Join(workspace, "alpha.go")
	betaPath := filepath.Join(workspace, "beta.go")
	writeLSPTestFile(t, alphaPath, "package sample\n")
	writeLSPTestFile(t, betaPath, "package sample\n")

	alphaURI, err := pathToDocumentURI(alphaPath)
	if err != nil {
		t.Fatalf("pathToDocumentURI(alpha) returned error: %v", err)
	}
	betaURI, err := pathToDocumentURI(betaPath)
	if err != nil {
		t.Fatalf("pathToDocumentURI(beta) returned error: %v", err)
	}

	client := &stubCheckClient{
		diagnosticsByURI: map[DocumentURI][]PublishDiagnosticsParams{
			betaURI: {
				{
					URI: betaURI,
					Diagnostics: []Diagnostic{
						{
							Range: Range{
								Start: Position{Line: 6, Character: 2},
							},
							Severity: DiagnosticSeverityError,
							Message:  "second failure",
						},
					},
				},
			},
			alphaURI: {
				{
					URI: alphaURI,
					Diagnostics: []Diagnostic{
						{
							Range: Range{
								Start: Position{Line: 1, Character: 0},
							},
							Severity: DiagnosticSeverityError,
							Message:  " first failure ",
						},
						{
							Range: Range{
								Start: Position{Line: 1, Character: 0},
							},
							Severity: DiagnosticSeverityError,
							Message:  " first failure ",
						},
					},
				},
			},
		},
	}

	result, err := NewCheckEngine(client).Run(context.Background(), CheckOptions{
		WorkspaceRoot:     workspace,
		TargetPath:        workspace,
		Language:          "go",
		DiagnosticTimeout: 20 * time.Millisecond,
		DiagnosticSettle:  5 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	want := "alpha.go\n  2:1  first failure\nbeta.go\n  7:3  second failure\n2 syntax errors found"
	if got := result.Output(); got != want {
		t.Fatalf("Output() = %q, want %q", got, want)
	}
}

func TestCheckEngineUsesLatestDiagnosticsForRepeatedNotifications(t *testing.T) {
	workspace := t.TempDir()
	filePath := filepath.Join(workspace, "main.go")
	writeLSPTestFile(t, filePath, "package sample\n")

	uri, err := pathToDocumentURI(filePath)
	if err != nil {
		t.Fatalf("pathToDocumentURI returned error: %v", err)
	}

	client := &stubCheckClient{
		diagnosticsByURI: map[DocumentURI][]PublishDiagnosticsParams{
			uri: {
				{
					URI: uri,
					Diagnostics: []Diagnostic{
						{
							Range: Range{
								Start: Position{Line: 2, Character: 1},
							},
							Severity: DiagnosticSeverityError,
							Message:  "stale failure",
						},
					},
				},
				{
					URI: uri,
					Diagnostics: []Diagnostic{
						{
							Range: Range{
								Start: Position{Line: 4, Character: 0},
							},
							Severity: DiagnosticSeverityError,
							Message:  "final failure",
						},
						{
							Range: Range{
								Start: Position{Line: 5, Character: 0},
							},
							Severity: DiagnosticSeverityWarning,
							Message:  "ignored warning",
						},
					},
				},
			},
		},
	}

	result, err := NewCheckEngine(client).Run(context.Background(), CheckOptions{
		WorkspaceRoot:     workspace,
		TargetPath:        filePath,
		Language:          "go",
		DiagnosticTimeout: 20 * time.Millisecond,
		DiagnosticSettle:  5 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if got := result.Output(); got != "main.go\n  5:1  final failure\n1 syntax error found" {
		t.Fatalf("Output() = %q", got)
	}
	if len(client.openedURIs) != 1 || client.openedURIs[0] != uri {
		t.Fatalf("opened URIs = %#v, want [%q]", client.openedURIs, uri)
	}
	if len(client.closedURIs) != 1 || client.closedURIs[0] != uri {
		t.Fatalf("closed URIs = %#v, want [%q]", client.closedURIs, uri)
	}
}

func TestCheckEngineDirectoryScanRespectsIgnoreRules(t *testing.T) {
	workspace := t.TempDir()
	writeLSPTestFile(t, filepath.Join(workspace, ".csignore"), "ignored.go\n")

	checkedPath := filepath.Join(workspace, "main.go")
	ignoredPath := filepath.Join(workspace, "ignored.go")
	writeLSPTestFile(t, checkedPath, "package sample\n")
	writeLSPTestFile(t, ignoredPath, "package sample\n")

	checkedURI, err := pathToDocumentURI(checkedPath)
	if err != nil {
		t.Fatalf("pathToDocumentURI returned error: %v", err)
	}

	client := &stubCheckClient{
		diagnosticsByURI: map[DocumentURI][]PublishDiagnosticsParams{
			checkedURI: {
				{URI: checkedURI},
			},
		},
	}

	result, err := NewCheckEngine(client).Run(context.Background(), CheckOptions{
		WorkspaceRoot:     workspace,
		TargetPath:        workspace,
		Language:          "go",
		DiagnosticTimeout: 20 * time.Millisecond,
		DiagnosticSettle:  5 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.ErrorCount() != 0 {
		t.Fatalf("ErrorCount() = %d, want 0", result.ErrorCount())
	}
	if len(client.openedURIs) != 1 || client.openedURIs[0] != checkedURI {
		t.Fatalf("opened URIs = %#v, want [%q]", client.openedURIs, checkedURI)
	}
}

func TestCheckEngineTimeoutWithoutDiagnosticsIsClean(t *testing.T) {
	workspace := t.TempDir()
	filePath := filepath.Join(workspace, "main.go")
	writeLSPTestFile(t, filePath, "package sample\n")

	client := &stubCheckClient{}
	result, err := NewCheckEngine(client).Run(context.Background(), CheckOptions{
		WorkspaceRoot:     workspace,
		TargetPath:        filePath,
		Language:          "go",
		DiagnosticTimeout: 15 * time.Millisecond,
		DiagnosticSettle:  5 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got := result.Output(); got != "No syntax errors found" {
		t.Fatalf("Output() = %q, want %q", got, "No syntax errors found")
	}
}

func TestCheckEngineReturnsErrorWhenFileCountExceedsMaxFiles(t *testing.T) {
	workspace := t.TempDir()
	for i := 0; i < 5; i++ {
		writeLSPTestFile(t, filepath.Join(workspace, fmt.Sprintf("file%d.go", i)), "package sample\n")
	}

	client := &stubCheckClient{}
	_, err := NewCheckEngine(client).Run(context.Background(), CheckOptions{
		WorkspaceRoot:     workspace,
		TargetPath:        workspace,
		Language:          "go",
		DiagnosticTimeout: 20 * time.Millisecond,
		DiagnosticSettle:  5 * time.Millisecond,
		MaxFiles:          3,
	})
	if err == nil {
		t.Fatal("expected max files error, got nil")
	}
	if !strings.Contains(err.Error(), "maximum is 3") {
		t.Fatalf("error = %q, want message containing 'maximum is 3'", err.Error())
	}
	if !strings.Contains(err.Error(), "found 5") {
		t.Fatalf("error = %q, want message containing 'found 5'", err.Error())
	}
}

func writeLSPTestFile(t *testing.T, path string, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) returned error: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%s) returned error: %v", path, err)
	}
}
