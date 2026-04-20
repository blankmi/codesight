package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"codesight/pkg/splitter"
)

const (
	defaultCheckDiagnosticTimeout = 250 * time.Millisecond
	defaultCheckDiagnosticSettle  = 40 * time.Millisecond
	defaultCheckMaxFiles          = 50
)

type checkClient interface {
	Notify(ctx context.Context, method string, params any) error
	SubscribeNotification(method string, handler func(json.RawMessage)) (func(), error)
}

// CheckOptions controls LSP-backed syntax checking.
type CheckOptions struct {
	WorkspaceRoot     string
	TargetPath        string
	Language          string
	DiagnosticTimeout time.Duration
	DiagnosticSettle  time.Duration
	MaxFiles          int
}

// CheckDiagnostic is one normalized error diagnostic.
type CheckDiagnostic struct {
	Path    string
	Line    int
	Column  int
	Message string
}

// CheckResult is the aggregated result of one syntax-check pass.
type CheckResult struct {
	Diagnostics []CheckDiagnostic
}

// ErrorCount returns the number of error diagnostics in the result.
func (r CheckResult) ErrorCount() int {
	return len(r.Diagnostics)
}

// Output formats the result for CLI output.
func (r CheckResult) Output() string {
	return formatCheckOutput(r.Diagnostics)
}

// CheckEngine gathers file diagnostics using LSP didOpen notifications.
type CheckEngine struct {
	client checkClient
}

// NewCheckEngine builds a new diagnostics engine.
func NewCheckEngine(client checkClient) *CheckEngine {
	return &CheckEngine{client: client}
}

// Run opens each matching file and collects error diagnostics reported by the server.
func (e *CheckEngine) Run(ctx context.Context, opts CheckOptions) (CheckResult, error) {
	if e == nil || e.client == nil {
		return CheckResult{}, errors.New("lsp client is required")
	}

	workspaceRoot, err := resolveWorkspaceRoot(opts.WorkspaceRoot)
	if err != nil {
		return CheckResult{}, err
	}

	targetPath := strings.TrimSpace(opts.TargetPath)
	if targetPath == "" {
		targetPath = workspaceRoot
	}
	targetPath, err = filepath.Abs(targetPath)
	if err != nil {
		return CheckResult{}, fmt.Errorf("resolve target path: %w", err)
	}
	targetPath = filepath.Clean(targetPath)

	language := normalizeLanguage(opts.Language)
	if language == "" {
		return CheckResult{}, errors.New("check language is required")
	}

	matcher, err := newWorkspaceIgnoreMatcher(workspaceRoot)
	if err != nil {
		return CheckResult{}, err
	}

	files, err := collectCheckFiles(workspaceRoot, targetPath, language, matcher)
	if err != nil {
		return CheckResult{}, err
	}

	maxFiles := opts.MaxFiles
	if maxFiles <= 0 {
		maxFiles = defaultCheckMaxFiles
	}
	if len(files) > maxFiles {
		return CheckResult{}, fmt.Errorf(
			"cs check: found %d %s files in %s, maximum is %d; pass individual files to check specific files",
			len(files), language, targetPath, maxFiles,
		)
	}

	notifications := make(chan PublishDiagnosticsParams, 64)
	unsubscribe, err := e.client.SubscribeNotification(MethodTextDocumentPublishDiagnostics, func(raw json.RawMessage) {
		var params PublishDiagnosticsParams
		if err := json.Unmarshal(raw, &params); err != nil {
			return
		}
		notifications <- params
	})
	if err != nil {
		return CheckResult{}, err
	}
	defer unsubscribe()

	diagnosticTimeout := opts.DiagnosticTimeout
	if diagnosticTimeout <= 0 {
		diagnosticTimeout = defaultCheckDiagnosticTimeout
	}
	diagnosticSettle := opts.DiagnosticSettle
	if diagnosticSettle <= 0 {
		diagnosticSettle = defaultCheckDiagnosticSettle
	}

	pending := make(map[DocumentURI]PublishDiagnosticsParams)
	allDiagnostics := make([]CheckDiagnostic, 0, 16)

	for _, filePath := range files {
		content, err := os.ReadFile(filePath)
		if err != nil {
			return CheckResult{}, fmt.Errorf("read %s: %w", filePath, err)
		}

		uri, err := pathToDocumentURI(filePath)
		if err != nil {
			return CheckResult{}, err
		}
		delete(pending, uri)

		if err := e.client.Notify(ctx, MethodTextDocumentDidOpen, DidOpenTextDocumentParams{
			TextDocument: TextDocumentItem{
				URI:        uri,
				LanguageID: language,
				Version:    1,
				Text:       string(content),
			},
		}); err != nil {
			return CheckResult{}, fmt.Errorf("open %s in lsp: %w", filePath, err)
		}

		params, err := waitForDiagnostics(ctx, notifications, pending, uri, diagnosticTimeout, diagnosticSettle)
		closeErr := e.client.Notify(ctx, MethodTextDocumentDidClose, DidCloseTextDocumentParams{
			TextDocument: TextDocumentIdentifier{URI: uri},
		})
		if err != nil {
			return CheckResult{}, err
		}
		if closeErr != nil {
			return CheckResult{}, fmt.Errorf("close %s in lsp: %w", filePath, closeErr)
		}

		allDiagnostics = append(allDiagnostics, diagnosticsToCheckLines(workspaceRoot, params.URI, params.Diagnostics)...)
	}

	return CheckResult{
		Diagnostics: dedupeAndSortCheckDiagnostics(allDiagnostics),
	}, nil
}

func collectCheckFiles(
	workspaceRoot string,
	targetPath string,
	language string,
	matcher interface{ MatchesPath(string) bool },
) ([]string, error) {
	info, err := os.Stat(targetPath)
	if err != nil {
		return nil, err
	}

	if !info.IsDir() {
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("target %s is not a regular file", targetPath)
		}
		if normalizeLanguage(splitter.LanguageFromExtension(filepath.Ext(targetPath))) != language {
			return nil, nil
		}
		return []string{targetPath}, nil
	}

	files := make([]string, 0, 32)
	err = filepath.WalkDir(targetPath, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.IsDir() {
			if path != targetPath && matcher != nil && matcher.MatchesPath(path) {
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
		if normalizeLanguage(splitter.LanguageFromExtension(filepath.Ext(path))) != language {
			return nil
		}

		rel := relativePath(workspaceRoot, path)
		if strings.HasPrefix(rel, "..") {
			return nil
		}

		files = append(files, filepath.Clean(path))
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Strings(files)
	return files, nil
}

func diagnosticsToCheckLines(workspaceRoot string, uri DocumentURI, diagnostics []Diagnostic) []CheckDiagnostic {
	path, err := documentURIToPath(uri)
	if err != nil {
		return nil
	}

	lines := make([]CheckDiagnostic, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity != 0 && diagnostic.Severity != DiagnosticSeverityError {
			continue
		}
		lines = append(lines, CheckDiagnostic{
			Path:    relativePath(workspaceRoot, path),
			Line:    diagnostic.Range.Start.Line + 1,
			Column:  diagnostic.Range.Start.Character + 1,
			Message: normalizeDiagnosticMessage(diagnostic.Message),
		})
	}
	return lines
}

func waitForDiagnostics(
	ctx context.Context,
	notifications <-chan PublishDiagnosticsParams,
	pending map[DocumentURI]PublishDiagnosticsParams,
	uri DocumentURI,
	timeout time.Duration,
	settle time.Duration,
) (PublishDiagnosticsParams, error) {
	if params, ok := pending[uri]; ok {
		delete(pending, uri)
		return settleDiagnostics(ctx, notifications, pending, uri, params, settle, timeout)
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return PublishDiagnosticsParams{}, fmt.Errorf("waiting for diagnostics: %w", ctx.Err())
		case params := <-notifications:
			if params.URI == uri {
				return settleDiagnostics(ctx, notifications, pending, uri, params, settle, timeout)
			}
			pending[params.URI] = params
		case <-timer.C:
			return PublishDiagnosticsParams{URI: uri}, nil
		}
	}
}

func settleDiagnostics(
	ctx context.Context,
	notifications <-chan PublishDiagnosticsParams,
	pending map[DocumentURI]PublishDiagnosticsParams,
	uri DocumentURI,
	initial PublishDiagnosticsParams,
	settle time.Duration,
	timeout time.Duration,
) (PublishDiagnosticsParams, error) {
	latest := initial

	settleTimer := time.NewTimer(settle)
	defer settleTimer.Stop()
	timeoutTimer := time.NewTimer(timeout)
	defer timeoutTimer.Stop()

	for {
		select {
		case <-ctx.Done():
			return PublishDiagnosticsParams{}, fmt.Errorf("waiting for diagnostics: %w", ctx.Err())
		case params := <-notifications:
			if params.URI == uri {
				latest = params
				if !settleTimer.Stop() {
					select {
					case <-settleTimer.C:
					default:
					}
				}
				settleTimer.Reset(settle)
				continue
			}
			pending[params.URI] = params
		case <-settleTimer.C:
			return latest, nil
		case <-timeoutTimer.C:
			return latest, nil
		}
	}
}

func pathToDocumentURI(path string) (DocumentURI, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve document path: %w", err)
	}

	normalized := filepath.ToSlash(absPath)
	if !strings.HasPrefix(normalized, "/") {
		normalized = "/" + normalized
	}

	return DocumentURI((&url.URL{
		Scheme: "file",
		Path:   normalized,
	}).String()), nil
}

func dedupeAndSortCheckDiagnostics(diagnostics []CheckDiagnostic) []CheckDiagnostic {
	if len(diagnostics) == 0 {
		return nil
	}

	sorted := append([]CheckDiagnostic(nil), diagnostics...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Path != sorted[j].Path {
			return sorted[i].Path < sorted[j].Path
		}
		if sorted[i].Line != sorted[j].Line {
			return sorted[i].Line < sorted[j].Line
		}
		if sorted[i].Column != sorted[j].Column {
			return sorted[i].Column < sorted[j].Column
		}
		return sorted[i].Message < sorted[j].Message
	})

	result := make([]CheckDiagnostic, 0, len(sorted))
	seen := make(map[string]struct{}, len(sorted))
	for _, diagnostic := range sorted {
		key := fmt.Sprintf("%s:%d:%d:%s", diagnostic.Path, diagnostic.Line, diagnostic.Column, diagnostic.Message)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, diagnostic)
	}

	return result
}

func formatCheckOutput(diagnostics []CheckDiagnostic) string {
	if len(diagnostics) == 0 {
		return "No syntax errors found"
	}

	lines := make([]string, 0, len(diagnostics)+2)
	currentPath := ""
	for _, diagnostic := range diagnostics {
		if diagnostic.Path != currentPath {
			currentPath = diagnostic.Path
			lines = append(lines, currentPath)
		}
		lines = append(lines, fmt.Sprintf("  %d:%d  %s", diagnostic.Line, diagnostic.Column, normalizeDiagnosticMessage(diagnostic.Message)))
	}

	label := "syntax errors"
	if len(diagnostics) == 1 {
		label = "syntax error"
	}
	lines = append(lines, fmt.Sprintf("%d %s found", len(diagnostics), label))
	return strings.Join(lines, "\n")
}

func normalizeDiagnosticMessage(message string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(message)), " ")
}
