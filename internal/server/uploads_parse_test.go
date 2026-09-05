package server

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// buildTestZip builds an in-memory zip from name→content pairs.
func buildTestZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sortStrings(names)
	for _, n := range names {
		w, err := zw.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(files[n])); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

const testSharedStrings = `<?xml version="1.0"?>
<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" count="3">
  <si><t>核算项目</t></si>
  <si><t>第一季度合计</t></si>
  <si><r><t>Rich </t></r><r><t>Text</t></r></si>
</sst>`

const testWorkbook = `<?xml version="1.0"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <sheets>
    <sheet name="2025年1期" sheetId="1"/>
    <sheet name="Empty" sheetId="2"/>
  </sheets>
</workbook>`

const testSheet1 = `<?xml version="1.0"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <sheetData>
    <row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c><c r="D1"><v>42.5</v></c></row>
    <row r="2"><c r="A2" t="inlineStr"><is><t>inline 值</t></is></c><c r="B2"><v>123</v></c></row>
  </sheetData>
</worksheet>`

const testSheet2 = `<?xml version="1.0"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <sheetData><row r="1"><c r="A1"><v>1</v></c></row></sheetData>
</worksheet>`

func TestExtractXLSXText(t *testing.T) {
	data := buildTestZip(t, map[string]string{
		"xl/sharedStrings.xml":     testSharedStrings,
		"xl/workbook.xml":          testWorkbook,
		"xl/worksheets/sheet1.xml": testSheet1,
		"xl/worksheets/sheet2.xml": testSheet2,
	})
	out, err := extractXLSXText(data)
	if err != nil {
		t.Fatalf("extractXLSXText: %v", err)
	}
	for _, want := range []string{
		"## 工作表: 2025年1期",
		"核算项目\t第一季度合计\t\t42.5", // D1 column placement preserved
		"inline 值\t123",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n---\n%s", want, out)
		}
	}
}

func TestParseOfficeTextDispatch(t *testing.T) {
	xlsx := buildTestZip(t, map[string]string{
		"xl/worksheets/sheet1.xml": testSheet1,
	})
	if s, ok := parseOfficeText("核算项目组合表.xlsx", xlsx); !ok || !strings.Contains(s, "工作表") {
		t.Errorf("xlsx not parsed: ok=%v out=%q", ok, s)
	}
	// Wrong extension → not dispatched.
	if _, ok := parseOfficeText("data.bin", xlsx); ok {
		t.Error("bin should not be dispatched to office parser")
	}
	// Garbage zip → falls back cleanly.
	if _, ok := parseOfficeText("a.xlsx", []byte("not a zip")); ok {
		t.Error("garbage xlsx should fail parse")
	}
}

func TestExtractDOCXText(t *testing.T) {
	docx := buildTestZip(t, map[string]string{
		"word/document.xml": `<?xml version="1.0"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>
    <w:p><w:r><w:t>第一段：项目说明</w:t></w:r></w:p>
    <w:p><w:r><w:t>第二段 </w:t></w:r><w:r><w:t>继续</w:t></w:r></w:p>
  </w:body>
</w:document>`,
	})
	out, err := extractDOCXText(docx)
	if err != nil {
		t.Fatalf("extractDOCXText: %v", err)
	}
	if !strings.Contains(out, "第一段：项目说明") || !strings.Contains(out, "第二段 继续") {
		t.Errorf("docx text missing paragraphs:\n%s", out)
	}
}

func TestMaterializeUploads(t *testing.T) {
	srcDir := t.TempDir()
	workDir := t.TempDir()
	src := filepath.Join(srcDir, "abc123.xlsx")
	if err := os.WriteFile(src, []byte("PK\x03\x04fake"), 0600); err != nil {
		t.Fatal(err)
	}

	got := materializeUploads([]uploadToMaterialize{{Name: "核算表.xlsx", Src: src}}, workDir)
	if got == "" {
		t.Fatal("expected non-empty summary")
	}
	dst := filepath.Join(workDir, ".magic-uploads", "abc123.xlsx")
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("materialized file missing: %v", err)
	}
	if string(data) != "PK\x03\x04fake" {
		t.Fatalf("content mismatch: %q", data)
	}
	if !strings.Contains(got, ".magic-uploads/abc123.xlsx") {
		t.Fatalf("summary should reference workdir-relative path, got: %s", got)
	}

	// Empty workdir / empty items are no-ops.
	if s := materializeUploads(nil, workDir); s != "" {
		t.Fatalf("expected empty summary for nil items, got %q", s)
	}
	if s := materializeUploads([]uploadToMaterialize{{Name: "x", Src: src}}, ""); s != "" {
		t.Fatalf("expected empty summary for empty workdir, got %q", s)
	}
}

// TestResolveExtensionNoSuffix ensures extensionless text uploads (LICENSE,
// Makefile, README...) do NOT fall through to ".bin". The sniff value from
// http.DetectContentType carries a charset parameter ("text/plain;
// charset=utf-8") that must be stripped before matching.
func TestResolveExtensionNoSuffix(t *testing.T) {
	cases := []struct {
		name  string
		sniff string
		want  string
	}{
		{"LICENSE", "text/plain; charset=utf-8", ".txt"},
		{"Makefile", "text/plain; charset=utf-8", ".txt"},
		{"plain text", "text/plain", ".txt"},
		{"html", "text/html; charset=utf-8", ".html"},
		{"csv", "text/csv", ".csv"},
		{"json", "application/json", ".json"},
		{"xml", "text/xml; charset=utf-8", ".xml"},
		{"pdf", "application/pdf", ".pdf"},
		{"png", "image/png", ".png"},
		{"zip", "application/zip", ".zip"},
		{"gzip", "application/x-gzip", ".gz"},
		{"unknown binary", "application/octet-stream", ".bin"},
		{"no sniff", "", ".bin"},
	}
	for _, c := range cases {
		if got := resolveExtension(c.name, c.sniff); got != c.want {
			t.Errorf("resolveExtension(%q, %q) = %q, want %q", c.name, c.sniff, got, c.want)
		}
	}
	// Client-supplied extensions always win, even when sniff says otherwise.
	if got := resolveExtension("notes.md", "text/plain; charset=utf-8"); got != ".md" {
		t.Errorf("client ext should win: got %q", got)
	}
}
