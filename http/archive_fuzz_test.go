package fbhttp

import (
	"path"
	"strings"
	"testing"
)

var hostileNames = []string{
	"../etc/passwd", "../../etc/passwd", "/etc/passwd", "//etc/passwd",
	"a/../../b", "a/./../../b", "....//....//etc", "..", ".", "",
	`..\..\evil.sh`, `C:\windows\system32`, `\\server\share\x`,
	"a//b", "a/b/../../../c", "./../../x", "foo/..", "foo/../..",
	"\x00/etc/passwd", "a\x00b", "..%2f..%2fetc", "\u002e\u002e/x",
	"a/\u202e/b", "\uff0e\uff0e/x", " ../x", "../ x", "-", "--",
	"~/.ssh/authorized_keys", "$HOME/x", "a/b/c/../../../../../../etc/shadow",
}

func FuzzSafeArchiveNameNeverEscapes(f *testing.F) {
	for _, s := range hostileNames {
		f.Add(s)
	}
	f.Add(strings.Repeat("../", 100) + "etc/passwd")
	f.Add(strings.Repeat("a/", 500) + "deep.txt")

	const dest = "/dest"

	f.Fuzz(func(t *testing.T, name string) {
		got, err := safeArchiveName(name)
		if err != nil || got == "" {
			return // rejected, or carries no member: either way nothing is written
		}

		joined := path.Join(dest, got)
		if joined != dest && !strings.HasPrefix(joined, dest+"/") {
			t.Fatalf("ESCAPE: safeArchiveName(%q) = %q -> %q is outside %q", name, got, joined, dest)
		}
		if strings.HasPrefix(got, "/") {
			t.Fatalf("ABSOLUTE: safeArchiveName(%q) = %q", name, got)
		}
		for _, part := range strings.Split(got, "/") {
			if part == ".." {
				t.Fatalf("TRAVERSAL SEGMENT: safeArchiveName(%q) = %q", name, got)
			}
		}
	})
}

func FuzzResolveInsideNeverEscapes(f *testing.F) {
	for _, s := range hostileNames {
		f.Add("/dest", s)
	}
	f.Add("/", "../x")
	f.Add("/a/b", "../../../etc/passwd")
	f.Add("/dest/", "x")

	f.Fuzz(func(t *testing.T, destDir, name string) {
		if destDir == "" {
			return
		}
		root := path.Clean(destDir)
		prefix := strings.TrimSuffix(root, "/") + "/"

		target, err := resolveInside(destDir, name)

		if err == nil {
			if target != root && !strings.HasPrefix(target, prefix) {
				t.Fatalf("ESCAPE: resolveInside(%q, %q) = %q is outside %q", destDir, name, target, root)
			}
			return
		}

		joined := path.Join(root, name)
		escapes := joined != root && !strings.HasPrefix(joined, prefix)
		if !escapes {
			t.Fatalf("OVER-REJECTED: resolveInside(%q, %q) errored, but %q is inside %q",
				destDir, name, joined, root)
		}
	})
}
