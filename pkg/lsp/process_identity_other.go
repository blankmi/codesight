//go:build !windows && !linux && !darwin

package lsp

func processStartID(pid int) (string, error) {
	_ = pid
	return "", errProcessIdentityUnsupported
}
