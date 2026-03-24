package engine

import "testing"

func TestClassify(t *testing.T) {
	tests := []struct {
		query    string
		override string
		want     QueryIntent
	}{
		// Symbol intent.
		{"Authenticate", "", IntentSymbol},
		{"auth.Login", "", IntentSymbol},
		{"ErrFoo", "", IntentSymbol},
		{"parseToken", "", IntentSymbol},
		{"MyStruct", "", IntentSymbol},

		// Path intent.
		{"pkg/auth.go", "", IntentPath},
		{"src/main.ts", "", IntentPath},
		{"auth.go", "", IntentPath},
		{"config.yaml", "", IntentPath},
		{"cmd/server/main.go", "", IntentPath},

		// Text intent.
		{"\"connection refused\"", "", IntentText},
		{"'timeout error'", "", IntentText},
		{"fatal error in auth", "", IntentText},
		{"cannot connect to database", "", IntentText},

		// AST intent.
		{"func {}", "", IntentAST},

		// Override.
		{"anything", "symbol", IntentSymbol},
		{"anything", "text", IntentText},
		{"anything", "path", IntentPath},
		{"anything", "ast", IntentAST},

		// Edge cases.
		{"", "", IntentText},
		{"a", "", IntentSymbol},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			got := Classify(tt.query, tt.override)
			if got != tt.want {
				t.Errorf("Classify(%q, %q) = %q, want %q", tt.query, tt.override, got, tt.want)
			}
		})
	}
}
