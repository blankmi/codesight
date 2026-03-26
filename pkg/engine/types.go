package engine

// SymbolIntelligence is the top-level result of a unified query.
type SymbolIntelligence struct {
	Query            string
	Mode             string // auto|symbol|text|ast|path
	Status           string // ok|ambiguous|not_found|not_found_exact|file_too_large
	Symbol           string
	Definition       *SymDefinition
	OtherDefinitions []SymDefinitionRef
	Ambiguous        []SymCandidate
	References       []SymReference
	Callers          []SymCaller
	Implementations  []SymImpl
	Meta             SymMeta
}

// SymDefinition describes the resolved definition of a symbol.
type SymDefinition struct {
	File         string
	Line         int
	EndLine      int
	Type         string // function, method, class, interface, struct, etc.
	Signature    string
	Docstring    string
	Body         string         // populated only if within budget
	Slices       []CodeSlice    // used for long bodies or top method expansions
	Outline      []OutlineEntry // member outline for class-like symbols
	ViewStrategy string         // full_body | signature_plus_slices | signature_plus_outline
	Language     string
}

// SymDefinitionRef is a compact reference to an alternate exact definition.
type SymDefinitionRef struct {
	File    string
	Line    int
	EndLine int
	Type    string
}

// OutlineEntry describes a member of a class-like symbol.
type OutlineEntry struct {
	Name       string // member name
	Kind       string // method, field, constructor, nested_type, etc.
	Line       int    // start line (1-indexed)
	EndLine    int    // end line (1-indexed)
	Signature  string // one-line signature
	Visibility string // public, private, protected, "" (unknown)
}

// CodeSlice is a labeled excerpt from a definition body.
type CodeSlice struct {
	Label     string
	StartLine int
	EndLine   int
	Reason    string
	Code      string
}

// SymReference is a scored reference to the queried symbol.
type SymReference struct {
	File    string
	Line    int
	Snippet string
	Score   float64
	Reason  string
}

// SymCaller is a scored incoming caller.
type SymCaller struct {
	Symbol string
	File   string
	Line   int
	Depth  int
	Score  float64
	Reason string
}

// SymImpl is a type/interface implementation.
type SymImpl struct {
	Name string
	File string
	Line int
}

// SymCandidate is a fallback suggestion when no exact match is found.
type SymCandidate struct {
	Name   string
	Type   string // function, file, const, etc.
	File   string
	Reason string
}

// SymBudget describes line allocation across output sections.
type SymBudget struct {
	TotalLines      int
	DefinitionLines int
	ReferenceLines  int
	CallerLines     int
	MetaLines       int
}

// SymMeta carries diagnostic and navigation metadata.
type SymMeta struct {
	Mode        string
	SearchChain []string
	Confidence  float64
	Budget      SymBudget
	RefsSource  string // lsp | grep | semantic
	RefsShown   int
	RefsTotal   int
	Errors      []string
	NextHint    string
}
