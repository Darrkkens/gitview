//go:build unix

package httpcol

import (
	"io/fs"
	"os"
	"syscall"
)

// ownedPrivate reports whether fi belongs to the current user and grants
// nothing to group or others.
func ownedPrivate(fi fs.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == os.Getuid() && fi.Mode().Perm()&0o077 == 0
}
