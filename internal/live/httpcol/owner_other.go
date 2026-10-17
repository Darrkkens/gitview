//go:build !unix

package httpcol

import "io/fs"

func ownedPrivate(fi fs.FileInfo) bool { return true }
