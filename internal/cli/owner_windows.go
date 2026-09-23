//go:build windows

package cli

import "os"

func ownedByRuntime(os.FileInfo) bool { return true }
