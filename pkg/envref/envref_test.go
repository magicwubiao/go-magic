package envref

import (
	"strings"
	"testing"
)

func TestExpandPlain(t *testing.T) {
	if got, err := Expand("plain-value"); err != nil || got != "plain-value" {
		t.Fatalf("Expand(plain) = %q, %v", got, err)
	}
	// 裸 $NAME 不展开：配置里出现 $ 的场景（正则、密码、SQL）太多，
	// 只有 ${...} 才是引用。
	if got, _ := Expand("a$HOME/b"); got != "a$HOME/b" {
		t.Fatalf("bare $ must stay literal, got %q", got)
	}
}

func TestExpandReference(t *testing.T) {
	t.Setenv("ENVREF_TEST_TOKEN", "s3cr3t")
	got, err := Expand("Bearer ${ENVREF_TEST_TOKEN}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "Bearer s3cr3t" {
		t.Fatalf("got %q", got)
	}
}

func TestExpandDefault(t *testing.T) {
	got, err := Expand("[${ENVREF_UNSET_A:-fallback}]")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "[fallback]" {
		t.Fatalf("got %q", got)
	}

	// 默认值为空串是合法意图：没配就用空，而不是报错。
	got, err = Expand("[${ENVREF_UNSET_B:-}]")
	if err != nil || got != "[]" {
		t.Fatalf("empty default: %q, %v", got, err)
	}
}

func TestExpandNestedDefault(t *testing.T) {
	t.Setenv("ENVREF_TEST_B", "inner")
	got, err := Expand("${ENVREF_UNSET_C:-${ENVREF_TEST_B}}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "inner" {
		t.Fatalf("got %q", got)
	}
}

func TestExpandMissingReportsName(t *testing.T) {
	_, err := Expand("${ENVREF_DEFINITELY_MISSING}")
	if err == nil {
		t.Fatal("expected error for missing variable")
	}
	if !strings.Contains(err.Error(), "ENVREF_DEFINITELY_MISSING") {
		t.Fatalf("error must name the variable, got %v", err)
	}
}

func TestExpandUnclosedBraceKept(t *testing.T) {
	got, err := Expand("abc ${UNCLOSED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "abc ${UNCLOSED" {
		t.Fatalf("unclosed brace must be kept verbatim, got %q", got)
	}
}

func TestExpandList(t *testing.T) {
	t.Setenv("ENVREF_TEST_TOKEN", "tok")
	got, err := ExpandList([]string{"TOKEN=${ENVREF_TEST_TOKEN}", "PLAIN=x", "NOEQUALS"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"TOKEN=tok", "PLAIN=x", "NOEQUALS"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestExpandMapDoesNotMutateInput(t *testing.T) {
	t.Setenv("ENVREF_TEST_TOKEN", "tok")
	in := map[string]string{"Authorization": "Bearer ${ENVREF_TEST_TOKEN}"}
	out, err := ExpandMap(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out["Authorization"] != "Bearer tok" {
		t.Fatalf("got %q", out["Authorization"])
	}
	if in["Authorization"] != "Bearer ${ENVREF_TEST_TOKEN}" {
		t.Fatalf("input was mutated: %q", in["Authorization"])
	}
}

func TestIsReference(t *testing.T) {
	cases := map[string]bool{
		"${GH_TOKEN}":            true,
		"  ${GH_TOKEN}  ":        true,
		"${GH_TOKEN:-abc}":       true,
		"Bearer ${GH_TOKEN}":     false,
		"${A}${B}":               false,
		"plain":                  false,
		"${A} tail":              false,
		"":                       false,
		"prefix${A}":             false,
		"${NESTED:-${FALLBACK}}": true,
	}
	for in, want := range cases {
		if got := IsReference(in); got != want {
			t.Errorf("IsReference(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestNameOf(t *testing.T) {
	if name, ok := NameOf("${GH_TOKEN}"); !ok || name != "GH_TOKEN" {
		t.Fatalf("NameOf = %q, %v", name, ok)
	}
	if name, ok := NameOf("${GH_TOKEN:-x}"); !ok || name != "GH_TOKEN" {
		t.Fatalf("NameOf with default = %q, %v", name, ok)
	}
	if _, ok := NameOf("Bearer ${GH_TOKEN}"); ok {
		t.Fatal("mixed value must not be a pure reference")
	}
}
