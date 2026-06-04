package fpm

import "os"

// SocketExists checks whether the given FPM socket path is present (service running).
// Pass resolved.Socket from php.ResolvePHP rather than a raw version string.
func SocketExists(socketPath string) bool {
	_, err := os.Stat(socketPath)
	return err == nil
}
