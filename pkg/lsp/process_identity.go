package lsp

import (
	"errors"
	"strings"
)

var errProcessIdentityUnsupported = errors.New("process identity unsupported")

func hasStrongProcessIdentity(state lifecycleState) bool {
	return strings.TrimSpace(state.DaemonProcessStartID) != ""
}

func processMatchesState(state lifecycleState) bool {
	expected := strings.TrimSpace(state.DaemonProcessStartID)
	if state.PID <= 0 || expected == "" {
		return false
	}

	startID, err := processStartID(state.PID)
	if err != nil {
		return false
	}
	return startID == expected
}

func refreshProcessStartID(state *lifecycleState) error {
	if state == nil || state.PID <= 0 {
		return nil
	}

	startID, err := processStartID(state.PID)
	if err != nil {
		if errors.Is(err, errProcessIdentityUnsupported) {
			return nil
		}
		return err
	}
	state.DaemonProcessStartID = startID
	return nil
}
