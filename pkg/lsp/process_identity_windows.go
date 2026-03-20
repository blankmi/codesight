//go:build windows

package lsp

func processStartID(pid int) (string, error) {
	_ = pid
	return "", errProcessIdentityUnsupported
}
