package lsp

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
)

type stubWorkspacePrimeClient struct {
	methods []string
	params  []DidOpenTextDocumentParams
}

func (s *stubWorkspacePrimeClient) Notify(_ context.Context, method string, params any) error {
	typed, ok := params.(DidOpenTextDocumentParams)
	if !ok {
		return fmt.Errorf("params type %T", params)
	}
	s.methods = append(s.methods, method)
	s.params = append(s.params, typed)
	return nil
}

func TestPrimeClientWorkspaceOpensFirstMatchingTypeScriptSource(t *testing.T) {
	workspace := t.TempDir()
	writeLSPTestFile(t, filepath.Join(workspace, ".csignore"), "ignored.ts\n")
	writeLSPTestFile(t, filepath.Join(workspace, "zeta.ts"), "export const zeta = 1\n")
	writeLSPTestFile(t, filepath.Join(workspace, "alpha.tsx"), "export const Alpha = () => null\n")
	writeLSPTestFile(t, filepath.Join(workspace, "ignored.ts"), "export const ignored = 1\n")

	client := &stubWorkspacePrimeClient{}
	if err := PrimeClientWorkspace(context.Background(), client, workspace, "typescript"); err != nil {
		t.Fatalf("PrimeClientWorkspace returned error: %v", err)
	}

	if len(client.methods) != 1 {
		t.Fatalf("notify calls = %d, want 1", len(client.methods))
	}
	if client.methods[0] != MethodTextDocumentDidOpen {
		t.Fatalf("notify method = %q, want %q", client.methods[0], MethodTextDocumentDidOpen)
	}
	if len(client.params) != 1 {
		t.Fatalf("didOpen params = %d, want 1", len(client.params))
	}

	got := client.params[0].TextDocument
	wantPath := filepath.Join(workspace, "alpha.tsx")
	wantURI, err := pathToDocumentURI(wantPath)
	if err != nil {
		t.Fatalf("pathToDocumentURI returned error: %v", err)
	}
	if got.URI != wantURI {
		t.Fatalf("opened URI = %q, want %q", got.URI, wantURI)
	}
	if got.LanguageID != "typescriptreact" {
		t.Fatalf("languageId = %q, want %q", got.LanguageID, "typescriptreact")
	}
	if got.Version != 1 {
		t.Fatalf("version = %d, want 1", got.Version)
	}
	if got.Text != "export const Alpha = () => null\n" {
		t.Fatalf("opened text = %q", got.Text)
	}
}

func TestPrimeClientWorkspaceNoopsWhenNoMatchingFilesExist(t *testing.T) {
	workspace := t.TempDir()
	writeLSPTestFile(t, filepath.Join(workspace, "main.go"), "package sample\n")

	client := &stubWorkspacePrimeClient{}
	if err := PrimeClientWorkspace(context.Background(), client, workspace, "typescript"); err != nil {
		t.Fatalf("PrimeClientWorkspace returned error: %v", err)
	}
	if len(client.methods) != 0 {
		t.Fatalf("notify calls = %d, want 0", len(client.methods))
	}
}

func TestPrimeClientWorkspaceSkipsLanguagesThatDoNotNeedPriming(t *testing.T) {
	workspace := t.TempDir()
	writeLSPTestFile(t, filepath.Join(workspace, "main.go"), "package sample\n")

	client := &stubWorkspacePrimeClient{}
	if err := PrimeClientWorkspace(context.Background(), client, workspace, "go"); err != nil {
		t.Fatalf("PrimeClientWorkspace returned error: %v", err)
	}
	if len(client.methods) != 0 {
		t.Fatalf("notify calls = %d, want 0", len(client.methods))
	}
}
