package tool

import (
	"net/url"
	"testing"
)

func TestParseKeyChord(t *testing.T) {
	tests := []struct {
		name  string
		input string
		key   string
		mods  int // input.Modifier is int64; compare on count of recognized forms instead
	}{
		{"ctrl+a", "Control+A", "a", 2},
		{"lower ctrl chord", "ctrl+shift+p", "p", 8 | 2},
		{"alt chord", "Alt+Enter", "Enter", 1},
		{"cmd chord", "Meta+Space", "Space", 4},
		{"plain key", "Enter", "Enter", 0},
		{"literal with plus", "hello+world", "hello+world", 0},
		{"leading modifier only", "ctrl", "ctrl", 0},
		{"unknown modifier", "hyper+a", "hyper+a", 0},
		{"dedupe", "ctrl+ctrl+a", "a", 2},
		{"single letter chord", "shift+a", "a", 8},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, mods := parseKeyChord(tt.input)
			if key != tt.key {
				t.Errorf("parseKeyChord(%q) key = %q, want %q", tt.input, key, tt.key)
			}
			got := 0
			for _, m := range mods {
				got |= int(m)
			}
			if got != tt.mods {
				t.Errorf("parseKeyChord(%q) mods = %d, want %d (%v)", tt.input, got, tt.mods, mods)
			}
		})
	}
}

func TestModifierFromName(t *testing.T) {
	for _, tc := range []struct {
		name string
		want int
		ok   bool
	}{
		{"ctrl", 2, true},
		{"control", 2, true},
		{"Ctrl", 2, true},
		{"shift", 8, true},
		{"alt", 1, true},
		{"meta", 4, true},
		{"windows", 4, true},
		{"bogus", 0, false},
	} {
		m, ok := modifierFromName(tc.name)
		if int(m) != tc.want || ok != tc.ok {
			t.Errorf("modifierFromName(%q) = (%d,%v), want (%d,%v)", tc.name, int(m), ok, tc.want, tc.ok)
		}
	}
}

func TestNormalizeURL(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://example.com/a", "https://example.com/a"},
		{"http://example.com", "http://example.com"},
		{"file:///D:/x.html", "file:///D:/x.html"},
		{"about:blank", "about:blank"},
		{"data:text/html,x", "data:text/html,x"},
		{"example.com/page", "https://example.com/page"},
		{"www.example.com", "https://www.example.com"},
		{"localhost:8080/x", "http://localhost:8080/x"},
		{"127.0.0.1:5173", "http://127.0.0.1:5173"},
		{"192.168.1.10/admin", "http://192.168.1.10/admin"},
		{"LOCALHOST", "http://LOCALHOST"},
	}
	for _, tt := range tests {
		if got := normalizeURL(tt.input); got != tt.want {
			t.Errorf("normalizeURL(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestLookLikeLocalHost(t *testing.T) {
	for _, yes := range []string{"localhost", "localhost:8080", "127.0.0.1", "127.0.0.1:9000", "::1", "10.0.0.5/path"} {
		if !looksLikeLocalHost(yes) {
			t.Errorf("looksLikeLocalHost(%q) = false, want true", yes)
		}
	}
	for _, no := range []string{"example.com", "example.com:443", "sub.domain.org/x", "fe80::1%lo0"} {
		if looksLikeLocalHost(no) {
			t.Errorf("looksLikeLocalHost(%q) = true, want false", no)
		}
	}
}

func TestJSQuote(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{`simple`, `"simple"`},
		{`it's`, `"it's"`},
		{`a"b`, `"a\"b"`},
		{"line\nbreak", `"line\nbreak"`},
		{`<script>alert(1)</script>`, `"<script>alert(1)</script>"`},
	}
	for _, tt := range tests {
		if got := jsQuote(tt.in); got != tt.want {
			t.Errorf("jsQuote(%q) = %s, want %s", tt.in, got, tt.want)
		}
	}
}

func TestResolveImgURL(t *testing.T) {
	base, _ := url.Parse("https://example.com/a/page.html")
	tests := []struct {
		src  string
		want string
	}{
		{"pic.png", "https://example.com/a/pic.png"},
		{"/pic.png", "https://example.com/pic.png"},
		{"pic.png?w=100", "https://example.com/a/pic.png?w=100"},
		{"https://cdn.example.com/x.png", "https://cdn.example.com/x.png"},
		{"//cdn.example.com/x.png", "https://cdn.example.com/x.png"},
		{"../up.png", "https://example.com/up.png"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := resolveImgURL(base, tt.src); got != tt.want {
			t.Errorf("resolveImgURL(%q) = %q, want %q", tt.src, got, tt.want)
		}
	}
	if got := resolveImgURL(nil, "x.png"); got != "x.png" {
		t.Errorf("resolveImgURL(nil base) = %q, want passthrough", got)
	}
}

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"default", "default"},
		{"../etc/passwd", ".._etc_passwd"},
		{`a:b*c?d"e<f>g|h`, "a_b_c_d_e_f_g_h"},
		{"   ", "page"},
		{"", "page"},
		{"..", "page"},
		{".", "page"},
		{"tab one", "tab one"},
	}
	for _, tt := range tests {
		if got := sanitizeFilename(tt.in); got != tt.want {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
