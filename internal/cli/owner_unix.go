//go:build linux || darwin

package cli

import (
	"os"
	"syscall"
)

func ownedByRuntime(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return !ok || int(stat.Uid) == os.Geteuid()
}
