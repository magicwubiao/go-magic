package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeKeyName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"enter", "Enter", "\r"},
		{"return", "return", "\r"},
		{"tab", "Tab", "\t"},
		{"escape", "Escape", "\u001b"},
		{"esc alias", "esc", "\u001b"},
		{"backspace", "Backspace", "\b"},
		{"delete", "Delete", "\u007f"},
		{"del alias", "DEL", "\u007f"},
		{"arrow up", "ArrowUp", "\u0304"},
		{"up alias", "up", "\u0304"},
		{"arrow down", "ArrowDown", "\u0301"},
		{"arrow left", "ArrowLeft", "\u0302"},
		{"arrow right", "ArrowRight", "\u0303"},
		{"home", "Home", "\u0306"},
		{"end", "End", "\u0305"},
		{"page up", "PageUp", "\u0308"},
		{"page down", "PageDown", "\u0307"},
		{"space", "Space", " "},
		{"caps lock", "CapsLock", "\u0104"},
		{"control", "Ctrl", "\u0105"},
		{"shift", "Shift", "\u010d"},
		{"alt", "Alt", "\u0102"},
		{"meta", "Meta", "\u0109"},
		{"f1", "F1", "\u0801"},
		{"f5", "f5", "\u0805"},
		{"f12", "F12", "\u080c"},
		{"plain text passthrough", "hello world", "hello world"},
		{"single char passthrough", "a", "a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeKeyName(tt.input); got != tt.expected {
				t.Errorf("normalizeKeyName(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestNormalizeKeyNameOutOfRange(t *testing.T) {
	// F13 and above are not supported by the mapping and pass through unchanged
	if got := normalizeKeyName("F13"); got != "F13" {
		t.Errorf("normalizeKeyName(F13) = %q, want passthrough", got)
	}
	if got := normalizeKeyName("f0"); got != "f0" {
		t.Errorf("normalizeKeyName(f0) = %q, want passthrough", got)
	}
}

func TestGuessImageExt(t *testing.T) {
	tests := []struct {
		contentType string
		url         string
		expected    string
	}{
		{"image/png", "http://example.com/img", ".png"},
		{"image/jpeg", "http://example.com/img", ".jpg"},
		{"image/gif", "http://example.com/img", ".gif"},
		{"image/webp", "http://example.com/img", ".webp"},
		{"", "http://example.com/photo.png", ".png"},
		{"", "http://example.com/photo.JPG", ".jpg"},
		{"", "http://example.com/photo", ".png"},
	}
	for _, tt := range tests {
		if got := guessImageExt(tt.contentType, tt.url); got != tt.expected {
			t.Errorf("guessImageExt(%q, %q) = %q, want %q", tt.contentType, tt.url, got, tt.expected)
		}
	}
}

func TestMimeForImage(t *testing.T) {
	tests := map[string]string{
		"/tmp/a.jpg":  "image/jpeg",
		"/tmp/a.jpeg": "image/jpeg",
		"/tmp/a.gif":  "image/gif",
		"/tmp/a.webp": "image/webp",
		"/tmp/a.bmp":  "image/bmp",
		"/tmp/a.png":  "image/png",
		"/tmp/no-ext": "image/png",
	}
	for path, want := range tests {
		if got := mimeForImage(path); got != want {
			t.Errorf("mimeForImage(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestTruncateStr(t *testing.T) {
	if got := truncateStr("hello", 10); got != "hello" {
		t.Errorf("truncateStr short = %q", got)
	}
	got := truncateStr("hello world", 5)
	if got != "hello..." {
		t.Errorf("truncateStr long = %q", got)
	}
	if got := truncateStr("你好世界", 2); got != "你好..." {
		t.Errorf("truncateStr multibyte = %q", got)
	}
}

func TestInspectImage(t *testing.T) {
	// Create a tiny valid PNG in memory
	png := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, // PNG signature
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52, // IHDR
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, // 1x1
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
		0x89, 0x00, 0x00, 0x00, 0x0a, 0x49, 0x44, 0x41,
		0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00,
		0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae,
		0x42, 0x60, 0x82,
	}

	dir := t.TempDir()
	path := dir + "/test.png"
	if err := os.WriteFile(path, png, 0644); err != nil {
		t.Fatalf("failed to write test image: %v", err)
	}

	info, err := inspectImage(path)
	if err != nil {
		t.Fatalf("inspectImage failed: %v", err)
	}
	if info["format"] != "png" {
		t.Errorf("format = %v, want png", info["format"])
	}
	if info["width"] != 1 || info["height"] != 1 {
		t.Errorf("dimensions = %vx%v, want 1x1", info["width"], info["height"])
	}

	if _, err := inspectImage(dir + "/missing.png"); err == nil {
		t.Error("expected error for missing file")
	}
}

func TestVisionToolRequiresSource(t *testing.T) {
	tool := NewVisionAnalyzeTool()
	_, err := tool.Execute(nil, map[string]interface{}{})
	if err == nil {
		t.Error("expected error when neither image_path nor image_url is provided")
	}
}

// TestResolveLocalImageWorkDirFallback locks the fix for the "first
// vision_analyze call fails at 0ms" report: attachments are materialized to
// <workDir>/.magic-uploads/, the model passes that workdir-relative path
// through, and the tool used to os.Stat it against the process cwd.
func TestResolveLocalImageWorkDirFallback(t *testing.T) {
	workDir := t.TempDir()
	rel := filepath.Join(".magic-uploads", "shot.png")
	abs := filepath.Join(workDir, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte("png"), 0644); err != nil {
		t.Fatal(err)
	}
	ctx := WithWorkDir(context.Background(), workDir)

	// Relative path that only exists under the workdir resolves there.
	got, err := resolveLocalMedia(ctx, rel)
	if err != nil {
		t.Fatalf("resolveLocalMedia(%q): %v", rel, err)
	}
	if got != abs {
		t.Errorf("resolved = %q, want %q", got, abs)
	}

	// Absolute path passes through untouched (normalized form on Windows).
	got, err = resolveLocalMedia(ctx, abs)
	if err != nil {
		t.Fatalf("resolveLocalMedia(abs): %v", err)
	}
	if got != filepath.Clean(abs) {
		t.Errorf("abs passthrough = %q, want %q", got, filepath.Clean(abs))
	}

	// Missing everywhere: the error must name both tried locations so the
	// model can self-correct.
	_, err = resolveLocalMedia(ctx, filepath.Join(".magic-uploads", "nope.png"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	msg := err.Error()
	if !strings.Contains(msg, "nope.png") || !strings.Contains(msg, workDir) {
		t.Errorf("error should mention the missing name and the session workdir, got: %v", err)
	}

	// No workdir in ctx (CLI-style): falls back to the literal path error.
	if _, err := resolveLocalMedia(context.Background(), "missing.png"); err == nil {
		t.Error("expected error when path missing and no workdir configured")
	}
}

// TestResolveLocalImagePrefersProcessCwd: when the relative path exists
// relative to the process cwd, it wins (backwards-compatible CLI behavior).
func TestResolveLocalImagePrefersProcessCwd(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "local.png"), []byte("png"), 0644); err != nil {
		t.Fatal(err)
	}
	// A workdir is configured but has no such file; the cwd copy must win.
	ctx := WithWorkDir(context.Background(), t.TempDir())

	got, err := resolveLocalMedia(ctx, "local.png")
	if err != nil {
		t.Fatalf("resolveLocalMedia: %v", err)
	}
	if want := filepath.Join(dir, "local.png"); got != want {
		t.Errorf("resolved = %q, want %q", got, want)
	}
}

// TestRegisterAllIncludesHermesParityTools verifies that the tools referenced
// from hermes-agent are actually registered by RegisterAll.
func TestRegisterAllIncludesHermesParityTools(t *testing.T) {
	r := NewRegistry()
	r.RegisterAll(t.TempDir())

	expected := []string{
		// New hermes-parity tools
		"browser_press", "browser_vision", "browser_dialog", "vision_analyze",
		// Previously implemented but unregistered browser tools
		"browser_forward", "browser_refresh", "browser_wait",
		"browser_get_info", "browser_clear_cache", "browser_get_cookies",
		// Terminal / process management
		"terminal", "process",
	}
	for _, name := range expected {
		if !r.HasTool(name) {
			t.Errorf("tool %q not registered by RegisterAll", name)
		}
	}
}
