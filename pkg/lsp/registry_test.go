package lsp

import (
	"errors"
	"strings"
	"testing"
)

func TestRegistryLookupKnownLanguage(t *testing.T) {
	registry := NewRegistry()

	spec, err := registry.Lookup("go")
	if err != nil {
		t.Fatalf("Lookup returned error: %v", err)
	}
	if spec.Language != "go" {
		t.Fatalf("Language = %q, want %q", spec.Language, "go")
	}
	if spec.Binary != "gopls" {
		t.Fatalf("Binary = %q, want %q", spec.Binary, "gopls")
	}
}

func TestRegistryLookupUnknownLanguage(t *testing.T) {
	registry := NewRegistry()

	_, err := registry.Lookup("elixir")
	if err == nil {
		t.Fatal("expected lookup error for unsupported language")
	}
	if !errors.Is(err, ErrUnsupportedLanguage) {
		t.Fatalf("error = %v, expected ErrUnsupportedLanguage", err)
	}
	if !strings.Contains(err.Error(), "supported:") {
		t.Fatalf("error %q does not include supported languages guidance", err.Error())
	}
}

func TestRegistryLookupReturnsIndependentArgsSlice(t *testing.T) {
	registry := NewRegistry()

	spec, err := registry.Lookup("typescript")
	if err != nil {
		t.Fatalf("Lookup returned error: %v", err)
	}
	spec.Args = append(spec.Args, "--mutated")

	specAgain, err := registry.Lookup("typescript")
	if err != nil {
		t.Fatalf("second Lookup returned error: %v", err)
	}
	for _, arg := range specAgain.Args {
		if arg == "--mutated" {
			t.Fatalf("registry args were mutated across lookups: %v", specAgain.Args)
		}
	}
}

func TestRegistryWithExtraServerArgsAppendsToLanguage(t *testing.T) {
	registry := NewRegistry(WithExtraServerArgs("java", "--jvm-arg=-javaagent:/tmp/lombok.jar"))

	spec, err := registry.Lookup("java")
	if err != nil {
		t.Fatalf("Lookup returned error: %v", err)
	}
	if len(spec.Args) != 1 || spec.Args[0] != "--jvm-arg=-javaagent:/tmp/lombok.jar" {
		t.Fatalf("Args = %#v, want the extra jvm arg appended", spec.Args)
	}

	tsSpec, err := registry.Lookup("typescript")
	if err != nil {
		t.Fatalf("Lookup returned error: %v", err)
	}
	if len(tsSpec.Args) != 1 || tsSpec.Args[0] != "--stdio" {
		t.Fatalf("typescript Args = %#v, want default args untouched", tsSpec.Args)
	}
}

func TestRegistryWithExtraServerArgsKeepsDefaultArgsFirst(t *testing.T) {
	registry := NewRegistry(WithExtraServerArgs("typescript", "--log-level", "4"))

	spec, err := registry.Lookup("typescript")
	if err != nil {
		t.Fatalf("Lookup returned error: %v", err)
	}
	want := []string{"--stdio", "--log-level", "4"}
	if len(spec.Args) != len(want) {
		t.Fatalf("Args = %#v, want %#v", spec.Args, want)
	}
	for i := range want {
		if spec.Args[i] != want[i] {
			t.Fatalf("Args = %#v, want %#v", spec.Args, want)
		}
	}
}

func TestRegistryWithExtraServerArgsIgnoresUnknownLanguage(t *testing.T) {
	registry := NewRegistry(WithExtraServerArgs("elixir", "--foo"))

	_, err := registry.Lookup("elixir")
	if !errors.Is(err, ErrUnsupportedLanguage) {
		t.Fatalf("error = %v, expected ErrUnsupportedLanguage", err)
	}
}
