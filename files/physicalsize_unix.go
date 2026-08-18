//go:build unix

package files

import (
	"io/fs"
	"syscall"
)

// statBlockSize is the unit st_blocks is reported in. POSIX fixes it at 512
// regardless of the filesystem's own block size.
const statBlockSize = 512

// physicalSize reports the space an entry actually occupies, as opposed to its
// logical length. The two diverge sharply on the ZFS datasets Horizon
// provisions: zstd compression takes the ASCII solver output down by 5-15x
// (a 385-column memory_usage.out measured 43.4MB long and 2.8MB on disk),
// while already-compressed Catalyst PNGs occupy slightly *more* than their
// length once block overhead is counted. Summed over a tree this agrees with
// du exactly, which is what an engineer will check it against over SSH.
//
// ok is false for afero filesystems that synthesize FileInfo rather than
// wrapping a real stat (MemMapFs in the tests); callers fall back to the
// logical size, which is the best available answer there.
func physicalSize(info fs.FileInfo) (size int64, ok bool) {
	st, isStat := info.Sys().(*syscall.Stat_t)
	if !isStat || st == nil {
		return 0, false
	}
	return int64(st.Blocks) * statBlockSize, true
}
