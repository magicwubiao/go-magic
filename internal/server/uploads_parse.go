package server

// Attachment text extraction for zip-based Office documents (xlsx/docx).
// The LLM cannot read raw zip bytes, so convertFilePart falls back to
// "binary — not readable" metadata and the model starts hunting for the
// file on disk (where it cannot reach). We instead parse the XML inside
// the container here and hand the model plain text. Hard caps keep
// pathological workbooks from blowing up the context window.

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	officeMaxSheets    = 5    // max worksheets extracted per xlsx
	officeMaxRows      = 500  // max rows per sheet
	officeMaxRowCells  = 64   // max cells per row
	officeMaxCellRunes = 2000 // max runes per cell value
	officeMaxOutBytes  = 96 << 10
)

// parseOfficeText dispatches by extension. ok=false means "not an office
// doc / parse failed / nothing extracted" — the caller falls back to the
// raw binary path.
func parseOfficeText(name string, data []byte) (string, bool) {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".xlsx", ".xlsm":
		if s, err := extractXLSXText(data); err == nil && strings.TrimSpace(s) != "" {
			return s, true
		}
	case ".docx":
		if s, err := extractDOCXText(data); err == nil && strings.TrimSpace(s) != "" {
			return s, true
		}
	}
	return "", false
}

// findZipFile locates an entry (case-insensitive prefix match on the
// canonical path) in the archive.
func findZipFile(zr *zip.Reader, name string) *zip.File {
	for _, f := range zr.File {
		if strings.EqualFold(f.Name, name) {
			return f
		}
	}
	return nil
}

// extractXLSXText converts the first few sheets of a workbook into TSV text.
// Shared strings, inline strings, formula string results and raw numbers are
// all resolved; cells are placed by their A1 column reference so empty cells
// do not shift columns.
func extractXLSXText(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", err
	}

	shared := readXSSFSharedStrings(zr)
	sheetNames := readXSSheetNames(zr)

	// Collect worksheet entries in stable numeric order.
	type sheetEntry struct {
		idx  int
		name string
	}
	var sheets []sheetEntry
	for _, f := range zr.File {
		if !strings.HasPrefix(f.Name, "xl/worksheets/sheet") || !strings.HasSuffix(f.Name, ".xml") {
			continue
		}
		mid := strings.TrimSuffix(strings.TrimPrefix(f.Name, "xl/worksheets/sheet"), ".xml")
		idx, err := strconv.Atoi(mid)
		if err != nil {
			continue
		}
		sheets = append(sheets, sheetEntry{idx: idx, name: f.Name})
	}
	if len(sheets) == 0 {
		return "", fmt.Errorf("no worksheet entries found")
	}
	sort.Slice(sheets, func(i, j int) bool { return sheets[i].idx < sheets[j].idx })
	if len(sheets) > officeMaxSheets {
		sheets = sheets[:officeMaxSheets]
	}

	var out bytes.Buffer
	for i, sf := range sheets {
		title := fmt.Sprintf("Sheet%d", i+1)
		if i < len(sheetNames) && sheetNames[i] != "" {
			title = sheetNames[i]
		}
		out.WriteString("## 工作表: " + title + "\n")
		rows := writeXSSheetRows(&out, zr, sf.name, shared)
		if rows >= officeMaxRows {
			out.WriteString("…(行数超过上限已截断)\n")
		}
		out.WriteString("\n")
		if out.Len() > officeMaxOutBytes {
			out.WriteString("…(内容过长已截断)\n")
			break
		}
	}
	return out.String(), nil
}

// readXSSFSharedStrings parses xl/sharedStrings.xml into a slice indexed by
// the <v> reference used in worksheet cells.
func readXSSFSharedStrings(zr *zip.Reader) []string {
	f := findZipFile(zr, "xl/sharedStrings.xml")
	if f == nil {
		return nil
	}
	rc, err := f.Open()
	if err != nil {
		return nil
	}
	defer rc.Close()

	var out []string
	var cur strings.Builder
	inSi, inT := false, false
	dec := xml.NewDecoder(rc)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "si":
				inSi, inT = true, false
				cur.Reset()
			case "t":
				if inSi {
					inT = true
				}
			}
		case xml.CharData:
			if inT {
				cur.Write(t)
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "si":
				out = append(out, cur.String())
				inSi, inT = false, false
			case "t":
				inT = false
			}
		}
	}
	return out
}

// readXSSheetNames parses xl/workbook.xml for the sheet display names, in
// workbook order.
func readXSSheetNames(zr *zip.Reader) []string {
	f := findZipFile(zr, "xl/workbook.xml")
	if f == nil {
		return nil
	}
	rc, err := f.Open()
	if err != nil {
		return nil
	}
	defer rc.Close()

	var out []string
	inSheet := false
	name := ""
	dec := xml.NewDecoder(rc)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "sheet" {
				inSheet = true
				name = ""
				for _, a := range t.Attr {
					if a.Name.Local == "name" {
						name = a.Value
					}
				}
			}
		case xml.EndElement:
			if t.Name.Local == "sheet" && inSheet {
				out = append(out, name)
				inSheet = false
			}
		}
	}
	return out
}

// xlsxColIndex converts the column letters of an A1 cell reference to a
// 0-based column index ("A1" → 0, "BC3" → 54).
func xlsxColIndex(ref string) int {
	n := 0
	for _, c := range ref {
		switch {
		case c >= 'A' && c <= 'Z':
			n = n*26 + int(c-'A'+1)
		case c >= 'a' && c <= 'z':
			n = n*26 + int(c-'a'+1)
		default:
			return n - 1
		}
	}
	return n - 1
}

// writeXSSheetRows streams one worksheet into TSV lines on out. Returns the
// number of rows written.
func writeXSSheetRows(out *bytes.Buffer, zr *zip.Reader, entry string, shared []string) int {
	f := findZipFile(zr, entry)
	if f == nil {
		return 0
	}
	rc, err := f.Open()
	if err != nil {
		return 0
	}
	defer rc.Close()

	dec := xml.NewDecoder(rc)
	rows := 0
	var row []string
	var cellVal, inline strings.Builder
	cellRef, cellType := "", ""
	inRow, inCell, inV, inIs, inT := false, false, false, false, false

	flushCell := func() {
		defer func() {
			inCell, inV, inIs, inT = false, false, false, false
			cellRef, cellType = "", ""
			cellVal.Reset()
			inline.Reset()
		}()
		if !inCell {
			return
		}
		var text string
		switch {
		case inline.Len() > 0:
			text = inline.String()
		case cellType == "s":
			if idx, err := strconv.Atoi(strings.TrimSpace(cellVal.String())); err == nil && idx >= 0 && idx < len(shared) {
				text = shared[idx]
			}
		default:
			text = cellVal.String()
		}
		if r := []rune(text); len(r) > officeMaxCellRunes {
			text = string(r[:officeMaxCellRunes]) + "…"
		}
		col := xlsxColIndex(cellRef)
		if col < 0 {
			col = len(row)
		}
		for len(row) <= col {
			row = append(row, "")
		}
		row[col] = text
		if len(row) >= officeMaxRowCells {
			row = row[:officeMaxRowCells]
		}
	}

	flushRow := func() {
		if !inRow {
			return
		}
		inRow = false
		for len(row) > 0 && row[len(row)-1] == "" {
			row = row[:len(row)-1]
		}
		if len(row) == 0 {
			return
		}
		out.WriteString(strings.Join(row, "\t") + "\n")
		rows++
		row = row[:0]
	}

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		if rows >= officeMaxRows || out.Len() > officeMaxOutBytes {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "row":
				inRow = true
			case "c":
				inCell = true
				for _, a := range t.Attr {
					switch a.Name.Local {
					case "r":
						cellRef = a.Value
					case "t":
						cellType = a.Value
					}
				}
			case "v":
				inV = true
				cellVal.Reset()
			case "is":
				inIs = true
				inline.Reset()
			case "t":
				if inIs {
					inT = true
				}
			}
		case xml.CharData:
			if inV {
				cellVal.Write(t)
			} else if inT {
				inline.Write(t)
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "v":
				inV = false
			case "t":
				inT = false
			case "is":
				inIs = false
			case "c":
				flushCell()
			case "row":
				// trailing cell without explicit </c> safety
				flushCell()
				flushRow()
			}
		}
	}
	// Flush any unterminated trailing row.
	flushCell()
	flushRow()
	return rows
}

// extractDOCXText concatenates the paragraph text of word/document.xml.
func extractDOCXText(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", err
	}
	f := findZipFile(zr, "word/document.xml")
	if f == nil {
		return "", fmt.Errorf("word/document.xml not found")
	}
	rc, err := f.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()

	var out bytes.Buffer
	var para strings.Builder
	inT := false
	dec := xml.NewDecoder(rc)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		if out.Len() > officeMaxOutBytes {
			out.WriteString("…(内容过长已截断)\n")
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "t":
				inT = true
			case "tab":
				para.WriteString("\t")
			case "br":
				para.WriteString("\n")
			}
		case xml.CharData:
			if inT {
				para.Write(t)
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inT = false
			case "p":
				if s := strings.TrimSpace(para.String()); s != "" {
					out.WriteString(s + "\n")
				}
				para.Reset()
			}
		}
	}
	return out.String(), nil
}
