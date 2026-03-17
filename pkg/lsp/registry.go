package lsp

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

var ErrUnsupportedLanguage = errors.New("unsupported language")

// ServerSpec describes how to launch the language server for one language.
type ServerSpec struct {
	Language    string
	Binary      string
	Args        []string
	InstallHint string
}

// Registry resolves language names to LSP server launch specs.
type Registry struct {
	servers map[string]ServerSpec
}

// NewRegistry builds the default language-server mapping used by refs/callers.
func NewRegistry() *Registry {
	return NewRegistryFromEntries(defaultRegistryEntries())
}

// NewRegistryFromEntries builds a registry from explicit entries.
func NewRegistryFromEntries(entries map[string]ServerSpec) *Registry {
	servers := make(map[string]ServerSpec, len(entries))
	for language, spec := range entries {
		key := normalizeLanguage(language)
		spec.Language = key
		spec.Args = append([]string(nil), spec.Args...)
		servers[key] = spec
	}

	return &Registry{servers: servers}
}

// Lookup returns the configured language server spec for a language.
func (r *Registry) Lookup(language string) (ServerSpec, error) {
	if r == nil {
		return ServerSpec{}, errors.New("registry is nil")
	}

	key := normalizeLanguage(language)
	spec, ok := r.servers[key]
	if !ok {
		return ServerSpec{}, fmt.Errorf(
			"%w %q; supported: %s",
			ErrUnsupportedLanguage,
			language,
			strings.Join(r.supportedLanguages(), ", "),
		)
	}

	spec.Args = append([]string(nil), spec.Args...)
	return spec, nil
}

func (r *Registry) supportedLanguages() []string {
	languages := make([]string, 0, len(r.servers))
	for language := range r.servers {
		languages = append(languages, language)
	}
	sort.Strings(languages)
	return languages
}

func normalizeLanguage(language string) string {
	return strings.ToLower(strings.TrimSpace(language))
}

func defaultRegistryEntries() map[string]ServerSpec {
	return map[string]ServerSpec{
		"go": {
			Language:    "go",
			Binary:      "gopls",
			InstallHint: "go install golang.org/x/tools/gopls@latest",
		},
		"python": {
			Language:    "python",
			Binary:      "pylsp",
			InstallHint: "pip install python-lsp-server",
		},
		"java": {
			Language:    "java",
			Binary:      "jdtls",
			InstallHint: "https://github.com/eclipse-jdtls/eclipse.jdt.ls",
		},
		"javascript": {
			Language:    "javascript",
			Binary:      "typescript-language-server",
			Args:        []string{"--stdio"},
			InstallHint: "npm install -g typescript typescript-language-server",
		},
		"typescript": {
			Language:    "typescript",
			Binary:      "typescript-language-server",
			Args:        []string{"--stdio"},
			InstallHint: "npm install -g typescript typescript-language-server",
		},
		"rust": {
			Language:    "rust",
			Binary:      "rust-analyzer",
			InstallHint: "rustup component add rust-analyzer",
		},
		"cpp": {
			Language:    "cpp",
			Binary:      "clangd",
			InstallHint: "install clangd from your package manager",
		},
	}
}
