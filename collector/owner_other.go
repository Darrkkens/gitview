//go:build !unix

package collector

import "io/fs"

// Ownership is not checked where the platform has no POSIX owners.
func ownedPrivate(fi fs.FileInfo) bool { return true }

func ownedBySelf(fi fs.FileInfo) bool { return true }
