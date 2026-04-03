package extract

// OutputFormat controls extractor output serialization.
type OutputFormat string

const (
	FormatRaw  OutputFormat = "raw"
	FormatJSON OutputFormat = "json"
)

// SymbolMatch is the canonical extraction payload for both file and directory modes.
type SymbolMatch struct {
	Name       string `json:"name"`
	Code       string `json:"code"`
	FilePath   string `json:"file_path"`
	StartLine  int    `json:"start_line"`
	EndLine    int    `json:"end_line"`
	StartByte  int    `json:"start_byte"`
	EndByte    int    `json:"end_byte"`
	SymbolType string `json:"symbol_type"`
}

// ListSymbol is the canonical symbol listing payload for list output.
type ListSymbol struct {
	Name       string `json:"name"`
	Code       string `json:"code"`
	FilePath   string `json:"file_path"`
	StartLine  int    `json:"start_line"`
	EndLine    int    `json:"end_line"`
	StartByte  int    `json:"start_byte"`
	EndByte    int    `json:"end_byte"`
	SymbolType string `json:"symbol_type"`
	LOC        int    `json:"loc"`
}

// ListResult contains rendered output plus recoverable warnings.
type ListResult struct {
	Output   string
	Warnings []string
}
