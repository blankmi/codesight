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
