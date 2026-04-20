package main

import (
	"testing"

	"codesight/pkg"
	configpkg "codesight/pkg/config"
	"codesight/pkg/engine"
	"codesight/pkg/vectorstore"
)

func TestNewQuerySemanticSearchAdapterUsesConfiguredCollectionName(t *testing.T) {
	cfg := configpkg.Defaults()
	cfg.DB.CollectionName = "shared_collection"

	var store vectorstore.Store = nil
	adapter := &engine.SemanticSearchAdapter{
		Searcher: &pkg.Searcher{
			Store:    store,
			Embedder: newEmbedder(cfg),
		},
		CollectionName: cfg.DB.CollectionName,
	}
	if adapter.CollectionName != "shared_collection" {
		t.Fatalf("collection name = %q, want %q", adapter.CollectionName, "shared_collection")
	}
	if adapter.Searcher == nil {
		t.Fatal("searcher is nil")
	}
}
