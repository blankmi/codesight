//go:build windows

package lsp

import "context"

func runDaemonFromEnv() error {
	return ErrDaemonDisabled
}

func launchDaemonProcess(ctx context.Context, config daemonProcessConfig) (int, error) {
	_ = ctx
	_ = config
	return 0, ErrDaemonDisabled
}

func daemonSocketHealthy(ctx context.Context, socketPath string) error {
	_ = ctx
	_ = socketPath
	return ErrDaemonDisabled
}

func shutdownDaemonViaSocket(ctx context.Context, socketPath string) error {
	_ = ctx
	_ = socketPath
	return ErrDaemonDisabled
}
