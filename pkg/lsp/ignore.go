package lsp

import (
	"fmt"

	csignore "github.com/blankbytes/codesight/pkg/ignore"
)

func newWorkspaceIgnoreMatcher(workspaceRoot string) (*csignore.Matcher, error) {
	matcher, err := csignore.NewMatcher(workspaceRoot, nil)
	if err != nil {
		return nil, fmt.Errorf("load ignore rules: %w", err)
	}
	return matcher, nil
}
