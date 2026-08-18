//go:build !unix

package files

import "io/fs"

// physicalSize has no portable implementation off unix: there is no stat field
// exposing the allocated block count. Callers fall back to the logical size.
// Releases are linux/amd64 only, so this exists to keep dev builds working.
func physicalSize(_ fs.FileInfo) (size int64, ok bool) {
	return 0, false
}
