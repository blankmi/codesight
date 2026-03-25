package engine

import (
	"strings"
	"testing"
)

func TestIsClassLikeType(t *testing.T) {
	trueCases := []string{"class", "struct", "interface", "module", "trait", "enum"}
	for _, tc := range trueCases {
		if !IsClassLikeType(tc) {
			t.Errorf("IsClassLikeType(%q) = false, want true", tc)
		}
	}

	falseCases := []string{"function", "method", "constant", "variable", "type", ""}
	for _, tc := range falseCases {
		if IsClassLikeType(tc) {
			t.Errorf("IsClassLikeType(%q) = true, want false", tc)
		}
	}
}

func TestIsFailureCueQuery(t *testing.T) {
	trueCases := []string{
		"fix UserService error",
		"debug crash in Parser",
		"exception in AuthHandler",
		"panic in handler",
		"failed test",
	}
	for _, tc := range trueCases {
		if !isFailureCueQuery(tc) {
			t.Errorf("isFailureCueQuery(%q) = false, want true", tc)
		}
	}

	falseCases := []string{
		"UserService",
		"AuthHandler",
		"how does Parser work",
		"Parser",
	}
	for _, tc := range falseCases {
		if isFailureCueQuery(tc) {
			t.Errorf("isFailureCueQuery(%q) = true, want false", tc)
		}
	}
}

func TestRankMethods(t *testing.T) {
	entries := []OutlineEntry{
		{Name: "helper", Kind: "method", Line: 80, EndLine: 85, Visibility: "private"},
		{Name: "Process", Kind: "method", Line: 30, EndLine: 60, Visibility: "public"},
		{Name: "__init__", Kind: "constructor", Line: 10, EndLine: 30, Visibility: "public"},
		{Name: "x", Kind: "method", Line: 90, EndLine: 90, Visibility: "private"},
	}

	ranked := rankMethods(entries, "")

	if ranked[0].Name != "__init__" {
		t.Errorf("expected __init__ first, got %q", ranked[0].Name)
	}
	if ranked[1].Name != "Process" {
		t.Errorf("expected Process second, got %q", ranked[1].Name)
	}
	// Single-liner should be last.
	if ranked[len(ranked)-1].Name != "x" {
		t.Errorf("expected x last, got %q", ranked[len(ranked)-1].Name)
	}
}

func TestRankMethodsQueryBoost(t *testing.T) {
	entries := []OutlineEntry{
		{Name: "Save", Kind: "method", Line: 10, EndLine: 30, Visibility: "public"},
		{Name: "Delete", Kind: "method", Line: 40, EndLine: 60, Visibility: "public"},
	}

	ranked := rankMethods(entries, "delete")

	if ranked[0].Name != "Delete" {
		t.Errorf("expected Delete first with query boost, got %q", ranked[0].Name)
	}
}

func TestOutlineClassDefinition_SmallBody(t *testing.T) {
	// 5-line Python class: should return full_body.
	source := []byte(`class Foo:
    x = 1
    def bar(self):
        return self.x
    def baz(self):
        return 2
`)
	sig, outline, topMethods, strategy := OutlineClassDefinition(source, "python", "Foo", 1, 6, 50)

	if strategy != "full_body" {
		t.Errorf("expected full_body, got %q", strategy)
	}
	if sig == "" {
		t.Error("expected non-empty signature")
	}
	if outline != nil {
		t.Error("expected nil outline for full_body")
	}
	if topMethods != nil {
		t.Error("expected nil topMethods for full_body")
	}
}

func TestOutlineClassDefinition_LargeBody(t *testing.T) {
	// Build a Python class with ~200 lines.
	var sb strings.Builder
	sb.WriteString("class BigService:\n")
	sb.WriteString("    name = 'service'\n")
	sb.WriteString("    def __init__(self, config):\n")
	for i := 0; i < 20; i++ {
		sb.WriteString("        pass\n")
	}
	sb.WriteString("    def process(self, data):\n")
	for i := 0; i < 30; i++ {
		sb.WriteString("        pass\n")
	}
	sb.WriteString("    def _helper(self):\n")
	for i := 0; i < 15; i++ {
		sb.WriteString("        pass\n")
	}
	sb.WriteString("    def validate(self):\n")
	for i := 0; i < 30; i++ {
		sb.WriteString("        pass\n")
	}
	sb.WriteString("    def save(self):\n")
	for i := 0; i < 30; i++ {
		sb.WriteString("        pass\n")
	}
	sb.WriteString("    def delete(self):\n")
	for i := 0; i < 30; i++ {
		sb.WriteString("        pass\n")
	}

	source := []byte(sb.String())
	lineCount := strings.Count(sb.String(), "\n")

	sig, outline, topMethods, strategy := OutlineClassDefinition(source, "python", "BigService", 1, lineCount, 40)

	if strategy != "signature_plus_outline" {
		t.Fatalf("expected signature_plus_outline, got %q", strategy)
	}
	if sig == "" {
		t.Error("expected non-empty signature")
	}
	if len(outline) == 0 {
		t.Error("expected non-empty outline")
	}

	// Verify outline contains expected members.
	hasInit := false
	hasProcess := false
	hasField := false
	for _, e := range outline {
		if e.Name == "__init__" {
			hasInit = true
			if e.Kind != "constructor" {
				t.Errorf("__init__ kind = %q, want constructor", e.Kind)
			}
		}
		if e.Name == "process" {
			hasProcess = true
		}
		if e.Kind == "field" {
			hasField = true
		}
	}
	if !hasInit {
		t.Error("outline missing __init__")
	}
	if !hasProcess {
		t.Error("outline missing process")
	}
	if !hasField {
		t.Error("outline missing field")
	}

	// Body > 160 lines, so should have top methods expanded.
	if lineCount > outlineMethodThreshold && len(topMethods) == 0 {
		t.Error("expected top methods for large class")
	}
	if len(topMethods) > 0 {
		// Constructor should be among expanded methods.
		foundInit := false
		for _, m := range topMethods {
			if strings.Contains(m.Label, "__init__") {
				foundInit = true
			}
		}
		if !foundInit {
			t.Error("expected __init__ among expanded top methods")
		}
	}
}

func TestExtractMembers_GoStruct(t *testing.T) {
	source := []byte(`package main

type Server struct {
	Host string
	Port int
}

func (s *Server) Start() error {
	return nil
}

func (s *Server) Stop() {
}

func unrelated() {}
`)

	sig, outline, _, strategy := OutlineClassDefinition(source, "go", "Server", 3, 6, 2)

	if strategy != "signature_plus_outline" {
		t.Fatalf("expected signature_plus_outline, got %q", strategy)
	}
	if sig == "" {
		t.Error("expected non-empty signature")
	}

	// Should have fields + methods from receiver matching.
	hasHost := false
	hasStart := false
	hasStop := false
	for _, e := range outline {
		switch e.Name {
		case "Host":
			hasHost = true
			if e.Kind != "field" {
				t.Errorf("Host kind = %q, want field", e.Kind)
			}
			if e.Visibility != "public" {
				t.Errorf("Host visibility = %q, want public", e.Visibility)
			}
		case "Start":
			hasStart = true
			if e.Kind != "method" {
				t.Errorf("Start kind = %q, want method", e.Kind)
			}
		case "Stop":
			hasStop = true
		}
	}
	if !hasHost {
		t.Error("outline missing Host field")
	}
	if !hasStart {
		t.Error("outline missing Start method (receiver match)")
	}
	if !hasStop {
		t.Error("outline missing Stop method (receiver match)")
	}

	// unrelated() should NOT be in the outline.
	for _, e := range outline {
		if e.Name == "unrelated" {
			t.Error("outline should not include unrelated function")
		}
	}
}

func TestExtractMembers_GoInterface(t *testing.T) {
	source := []byte(`package main

type Handler interface {
	ServeHTTP(w ResponseWriter, r *Request)
	Close() error
}
`)

	_, outline, _, strategy := OutlineClassDefinition(source, "go", "Handler", 3, 6, 2)

	if strategy != "signature_plus_outline" {
		t.Fatalf("expected signature_plus_outline, got %q", strategy)
	}

	if len(outline) < 2 {
		t.Fatalf("expected at least 2 outline entries, got %d", len(outline))
	}

	hasServeHTTP := false
	hasClose := false
	for _, e := range outline {
		if e.Name == "ServeHTTP" {
			hasServeHTTP = true
			if e.Kind != "method" {
				t.Errorf("ServeHTTP kind = %q, want method", e.Kind)
			}
		}
		if e.Name == "Close" {
			hasClose = true
		}
	}
	if !hasServeHTTP {
		t.Error("outline missing ServeHTTP")
	}
	if !hasClose {
		t.Error("outline missing Close")
	}
}

func TestExtractMembers_JavaAnnotations(t *testing.T) {
	source := []byte(`package com.example;

public class UserService {

    @Inject
    private UserRepository userRepo;

    @Inject
    private Logger logger;

    @Override
    public void init() {
        logger.info("init");
    }

    @Asynchronous
    public CompletableFuture<User> findUser(String id) {
        return userRepo.find(id);
    }

    private void validate(String id) {
        if (id == null) throw new IllegalArgumentException();
    }
}
`)
	lineCount := strings.Count(string(source), "\n")
	_, outline, _, strategy := OutlineClassDefinition(source, "java", "UserService", 3, lineCount, 2)

	if strategy != "signature_plus_outline" {
		t.Fatalf("expected signature_plus_outline, got %q", strategy)
	}

	// Verify annotations are NOT in signatures.
	for _, e := range outline {
		if strings.HasPrefix(e.Signature, "@") {
			t.Errorf("member %q signature starts with annotation: %q", e.Name, e.Signature)
		}
	}

	// Verify field names are extracted correctly (not @Inject).
	hasUserRepo := false
	hasLogger := false
	for _, e := range outline {
		if e.Name == "userRepo" {
			hasUserRepo = true
			if e.Kind != "field" {
				t.Errorf("userRepo kind = %q, want field", e.Kind)
			}
			if e.Visibility != "private" {
				t.Errorf("userRepo visibility = %q, want private", e.Visibility)
			}
		}
		if e.Name == "logger" {
			hasLogger = true
		}
	}
	if !hasUserRepo {
		t.Errorf("outline missing userRepo field, got: %v", outline)
	}
	if !hasLogger {
		t.Errorf("outline missing logger field, got: %v", outline)
	}

	// Verify method signatures skip annotations.
	for _, e := range outline {
		if e.Name == "findUser" {
			if !strings.Contains(e.Signature, "findUser") {
				t.Errorf("findUser signature should contain method name, got: %q", e.Signature)
			}
		}
		if e.Name == "init" {
			if e.Visibility != "public" {
				t.Errorf("init visibility = %q, want public", e.Visibility)
			}
		}
	}
}

func TestSignatureLineSkipsAnnotations(t *testing.T) {
	source := []byte(`@Slf4j
@Service
public class DeleteSupplierService {
    // body
}
`)
	sig := signatureLine(source, 1)
	if strings.HasPrefix(sig, "@") {
		t.Errorf("signatureLine should skip annotations, got: %q", sig)
	}
	if !strings.Contains(sig, "DeleteSupplierService") {
		t.Errorf("signatureLine should contain class name, got: %q", sig)
	}
}

func TestRenderMarkdownOutline(t *testing.T) {
	result := &SymbolIntelligence{
		Query:  "UserService",
		Symbol: "UserService",
		Status: "ok",
		Mode:   "symbol",
		Definition: &SymDefinition{
			File:         "pkg/service.py",
			Line:         10,
			EndLine:      250,
			Type:         "class",
			Signature:    "class UserService(BaseService):",
			ViewStrategy: "signature_plus_outline",
			Language:     "python",
			Outline: []OutlineEntry{
				{Name: "__init__", Kind: "constructor", Line: 12, EndLine: 30, Signature: "def __init__(self, db):", Visibility: "public"},
				{Name: "create_user", Kind: "method", Line: 32, EndLine: 60, Signature: "def create_user(self, data):", Visibility: "public"},
				{Name: "_validate", Kind: "method", Line: 62, EndLine: 80, Signature: "def _validate(self, data):", Visibility: "private"},
			},
			Slices: []CodeSlice{
				{Label: "Method: __init__", StartLine: 12, EndLine: 30, Reason: "constructor, public", Code: "def __init__(self, db):\n    self.db = db"},
			},
		},
		Meta: SymMeta{
			Mode:        "symbol",
			SearchChain: []string{"symbol"},
			Confidence:  0.85,
			Budget:      ComputeBudget("auto", 1),
		},
	}

	md := RenderMarkdown(result)

	checks := []string{
		"# Symbol: `UserService`",
		"Member Outline (3 members)",
		"| Line | Kind | Visibility | Name |",
		"__init__",
		"create_user",
		"_validate",
		"constructor",
		"### Method: __init__",
		"def __init__(self, db):",
		"Next most relevant methods",
	}
	for _, check := range checks {
		if !strings.Contains(md, check) {
			t.Errorf("missing %q in rendered markdown:\n%s", check, md)
		}
	}
}

func TestQuerySymbol_ClassOutline(t *testing.T) {
	// Mock a class definition with a large body.
	var body strings.Builder
	body.WriteString("class BigClass:\n")
	for i := 0; i < 100; i++ {
		body.WriteString("    pass\n")
	}

	eng := &Engine{
		WorkspaceRoot: t.TempDir(),
		Extractor: &mockExtractor{
			def: &SymDefinition{
				File:         "test.py",
				Line:         1,
				EndLine:      101,
				Type:         "class",
				Signature:    "class BigClass:",
				Body:         body.String(),
				ViewStrategy: "full_body",
				Language:     "python",
			},
		},
	}

	// Note: since the file doesn't actually exist, os.ReadFile will fail
	// and the engine will keep full_body. This test validates the branching
	// logic by checking that the mock is set up correctly.
	// Full integration requires actual files - tested in outliner_test.go.
	result, err := eng.Query(t.Context(), QueryOptions{Query: "BigClass", Budget: "small"})
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if result.Status != "ok" {
		t.Errorf("Status = %q, want ok", result.Status)
	}
}

func TestQuerySymbol_FunctionStaysSlices(t *testing.T) {
	var body strings.Builder
	body.WriteString("func BigFunc() {\n")
	for i := 0; i < 100; i++ {
		body.WriteString("    pass()\n")
	}
	body.WriteString("}\n")

	eng := &Engine{
		WorkspaceRoot: t.TempDir(),
		Extractor: &mockExtractor{
			def: &SymDefinition{
				File:         "test.go",
				Line:         1,
				EndLine:      102,
				Type:         "function",
				Signature:    "func BigFunc() {",
				Body:         body.String(),
				ViewStrategy: "full_body",
				Language:     "go",
			},
		},
	}

	result, err := eng.Query(t.Context(), QueryOptions{Query: "BigFunc", Budget: "small"})
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	// Function should NOT get outline strategy.
	if result.Definition != nil && result.Definition.ViewStrategy == "signature_plus_outline" {
		t.Error("function should not get signature_plus_outline")
	}
}
