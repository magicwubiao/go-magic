package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWithin(t *testing.T) {
	sep := string(filepath.Separator)
	cases := []struct {
		dir    string
		target string
		want   bool
	}{
		{"/work", "/work", true},
		{"/work", "/work/a.txt", true},
		{"/work", "/work/sub/a.txt", true},
		// The prefix bug: "/work-evil" must NOT count as a child of "/work".
		{"/work", "/work" + "-evil/a.txt", false},
		{"/work", "/other/a.txt", false},
		{"/work", "/work/../etc/passwd", false},
		{"/work", "/work/..", false},
		{"/work", "/" + "etc" + sep + "passwd", false},
	}
	for _, c := range cases {
		if got := within(c.dir, c.target); got != c.want {
			t.Errorf("within(%q, %q) = %v, want %v", c.dir, c.target, got, c.want)
		}
	}
}

func TestBasicSandboxResolvePath(t *testing.T) {
	root := t.TempDir()
	// A sibling directory sharing a name prefix with the sandbox root. The old
	// strings.HasPrefix check wrongly allowed writes into it.
	sibling := root + "-sibling"
	if err := os.MkdirAll(sibling, 0o755); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(sibling)

	s := NewBasicSandbox(root)

	// Relative paths must land inside the sandbox (they used to fail outright,
	// because filepath.Abs resolved them against the process CWD).
	got, err := s.resolvePath("a.txt")
	if err != nil {
		t.Fatalf("resolvePath(a.txt) unexpected error: %v", err)
	}
	if want := filepath.Join(root, "a.txt"); got != want {
		t.Errorf("resolvePath(a.txt) = %q, want %q", got, want)
	}

	// A leading slash stays relative to the sandbox root.
	got, err = s.resolvePath("/b.txt")
	if err != nil {
		t.Fatalf("resolvePath(/b.txt) unexpected error: %v", err)
	}
	if want := filepath.Join(root, "b.txt"); got != want {
		t.Errorf("resolvePath(/b.txt) = %q, want %q", got, want)
	}

	// Escapes must be rejected.
	for _, bad := range []string{"../outside.txt", "../../etc/passwd", "sub/../../outside.txt"} {
		if _, err := s.resolvePath(bad); err == nil {
			t.Errorf("resolvePath(%q) succeeded, want rejection", bad)
		}
	}

	// The prefix-sharing sibling must be rejected.
	if _, err := s.resolvePath("../" + filepath.Base(sibling) + "/x.txt"); err == nil {
		t.Errorf("resolvePath into prefix-sharing sibling succeeded, want rejection")
	}

	// End-to-end: a relative write/read round-trip now works.
	if err := s.WriteFile("nested.txt", []byte("hello")); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	data, err := s.ReadFile("nested.txt")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("ReadFile = %q, want %q", data, "hello")
	}
	if _, err := os.Stat(filepath.Join(root, "nested.txt")); err != nil {
		t.Errorf("file was not written inside the sandbox root: %v", err)
	}

	// Symlink escape must be caught.
	link := filepath.Join(root, "escape")
	if err := os.Symlink(os.TempDir(), link); err == nil {
		if _, err := s.resolvePath("escape/evil.txt"); err == nil {
			t.Errorf("resolvePath through an escaping symlink succeeded, want rejection")
		}
	}
}

func TestDockerSandboxContainerPath(t *testing.T) {
	d := NewDockerSandbox("")

	cases := []struct {
		in     string
		want   string
		reject bool
	}{
		{in: "a.txt", want: "/workspace/a.txt"},
		{in: "/a.txt", want: "/workspace/a.txt"},
		{in: "sub/dir/a.txt", want: "/workspace/sub/dir/a.txt"},
		// Traversal is rejected loudly rather than silently clamped: cleaning
		// "sub/../a.txt" to "/workspace/a.txt" would hide a caller bug.
		{in: "sub/../a.txt", reject: true},
		{in: "../outside.txt", reject: true},
		{in: "../../etc/passwd", reject: true},
		{in: "", reject: true},
		{in: "   ", reject: true},
	}
	for _, c := range cases {
		got, err := d.containerPath(c.in)
		if c.reject {
			if err == nil {
				t.Errorf("containerPath(%q) = %q, want rejection", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("containerPath(%q) unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("containerPath(%q) = %q, want %q", c.in, got, c.want)
		}
		if !strings.HasPrefix(got, containerWorkDir+"/") {
			t.Errorf("containerPath(%q) = %q, must stay under %s", c.in, got, containerWorkDir)
		}
	}
}
