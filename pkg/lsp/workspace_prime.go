package lsp

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"codesight/pkg/splitter"
)

type workspacePrimeClient interface {
	Notify(ctx context.Context, method string, params any) error
}

// PrimeClientWorkspace opens one real workspace source file for language servers
// that need file-backed project context before workspace-wide symbol queries.
func PrimeClientWorkspace(ctx context.Context, client workspacePrimeClient, workspaceRoot, language string) error {
	if client == nil {
		return errors.New("lsp client is nil")
	}
	if !languageRequiresWorkspacePrime(language) {
		return nil
	}

	workspaceRoot, err := resolveWorkspaceRoot(workspaceRoot)
	if err != nil {
		return err
	}

	filePath, err := workspacePrimeFile(workspaceRoot, language)
	if err != nil {
		return err
	}
	if filePath == "" {
		return nil
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read %s: %w", filePath, err)
	}

	uri, err := pathToDocumentURI(filePath)
	if err != nil {
		return err
	}

	if err := client.Notify(ctx, MethodTextDocumentDidOpen, DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{
			URI:        uri,
			LanguageID: workspacePrimeLanguageID(language, filePath),
			Version:    1,
			Text:       string(content),
		},
	}); err != nil {
		return fmt.Errorf("prime workspace document %s: %w", filePath, err)
	}

	return nil
}

func workspacePrimeFile(workspaceRoot, language string) (string, error) {
	matcher, err := newWorkspaceIgnoreMatcher(workspaceRoot)
	if err != nil {
		return "", err
	}

	files := make([]string, 0, 8)
	err = filepath.WalkDir(workspaceRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != workspaceRoot && matcher != nil && matcher.MatchesPath(path) {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		if matcher != nil && matcher.MatchesPath(path) {
			return nil
		}
		if !workspacePrimeLanguageMatches(language, splitter.LanguageFromExtension(filepath.Ext(path))) {
			return nil
		}

		files = append(files, filepath.Clean(path))
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "", nil
	}

	sort.Strings(files)
	return files[0], nil
}

func languageRequiresWorkspacePrime(language string) bool {
	switch normalizeLanguage(language) {
	case "javascript", "typescript":
		return true
	default:
		return false
	}
}

func workspacePrimeLanguageMatches(target, candidate string) bool {
	switch normalizeLanguage(target) {
	case "typescript":
		return candidate == "typescript" || candidate == "tsx"
	case "javascript":
		return candidate == "javascript" || candidate == "jsx"
	default:
		return normalizeLanguage(candidate) == normalizeLanguage(target)
	}
}

func workspacePrimeLanguageID(language string, filePath string) string {
	switch splitter.LanguageFromExtension(filepath.Ext(filePath)) {
	case "tsx":
		return "typescriptreact"
	case "jsx":
		return "javascriptreact"
	default:
		return normalizeLanguage(language)
	}
}
