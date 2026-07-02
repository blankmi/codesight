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

// RegistryOption customizes the registry built by NewRegistry.
type RegistryOption func(map[string]ServerSpec)

// WithExtraServerArgs appends launch arguments to one language's server spec.
// Languages without a default spec are ignored.
func WithExtraServerArgs(language string, args ...string) RegistryOption {
	return func(entries map[string]ServerSpec) {
		key := normalizeLanguage(language)
		spec, ok := entries[key]
		if !ok || len(args) == 0 {
			return
		}
		spec.Args = append(append([]string(nil), spec.Args...), args...)
		entries[key] = spec
	}
}

// NewRegistry builds the default language-server mapping used by refs/callers.
func NewRegistry(opts ...RegistryOption) *Registry {
	entries := defaultRegistryEntries()
	for _, opt := range opts {
		if opt != nil {
			opt(entries)
		}
	}
	return NewRegistryFromEntries(entries)
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
