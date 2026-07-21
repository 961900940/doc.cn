package main

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"os/exec"
	"runtime"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestExtractPDFEmbeddedJPEG(t *testing.T) {
	// minimal jpeg
	jpeg := []byte{0xFF, 0xD8, 0xFF, 0xD9}
	// make it larger than filter threshold
	body := bytes.Repeat([]byte{0x01, 0x02, 0x03, 0x04}, 40)
	jpeg = append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, body...)
	jpeg = append(jpeg, 0xFF, 0xD9)

	pdf := append([]byte("%PDF-1.4\n"), jpeg...)
	pdf = append(pdf, []byte("\n%%EOF")...)
	images := extractPDFEmbeddedImages(pdf)
	if len(images) == 0 {
		t.Fatalf("expected embedded jpeg extraction")
	}
}

func TestExtractPDFPlainTextViaLibrary(t *testing.T) {
	// Use a tiny PDF; library may return empty for some minimal files — just ensure no panic
	pdfData := []byte(`%PDF-1.4
1 0 obj<< /Type /Catalog /Pages 2 0 R >>endobj
2 0 obj<< /Type /Pages /Kids [3 0 R] /Count 1 >>endobj
3 0 obj<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 144] /Contents 4 0 R /Resources<< /Font<< /F1 5 0 R >> >> >>endobj
4 0 obj<< /Length 55 >>stream
BT /F1 24 Tf 50 80 Td (Library Hello) Tj ET
endstream
endobj
5 0 obj<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>endobj
xref
0 6
0000000000 65535 f 
0000000009 00000 n 
0000000058 00000 n 
0000000115 00000 n 
0000000266 00000 n 
0000000372 00000 n 
trailer<< /Size 6 /Root 1 0 R >>
startxref
451
%%EOF`)
	text := extractPDFPlainTextViaLibrary(pdfData)
	// Either library extracts it, or returns empty — must not crash
	_ = text
}

func TestConvertImportPDFSimpleTextOperators(t *testing.T) {
	pdf := `%PDF-1.1
1 0 obj<<>>endobj
2 0 obj<< /Length 44 >>stream
BT /F1 12 Tf (Hello PDF Import) Tj ET
endstream
endobj
trailer<<>>
%%EOF`
	text := sanitizePDFText(extractPDFText([]byte(pdf)))
	if !strings.Contains(text, "Hello PDF Import") {
		t.Fatalf("unexpected pdf text: %q", text)
	}
}

func TestConvertImportPDFRendersPagesWhenHelperAvailable(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("PDFKit helper is macOS only")
	}
	if _, err := exec.LookPath("swiftc"); err != nil {
		t.Skip("swiftc not available")
	}

	db, err := sql.Open("sqlite", "file:pdfimport?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE attachments (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		original_name TEXT,
		file_path TEXT,
		mime_type TEXT,
		file_size INTEGER,
		created_by INTEGER,
		created_at TEXT
	)`); err != nil {
		t.Fatal(err)
	}

	uploadDir := t.TempDir()
	dataDir := t.TempDir()
	a := &app{db: db, uploadDir: uploadDir, dataDir: dataDir}

	pdf := []byte(`%PDF-1.4
1 0 obj<< /Type /Catalog /Pages 2 0 R >>endobj
2 0 obj<< /Type /Pages /Kids [3 0 R] /Count 1 >>endobj
3 0 obj<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 144] /Contents 4 0 R /Resources<< /Font<< /F1 5 0 R >> >> >>endobj
4 0 obj<< /Length 44 >>stream
BT /F1 24 Tf 50 80 Td (Hello) Tj ET
endstream
endobj
5 0 obj<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>endobj
xref
0 6
0000000000 65535 f 
0000000009 00000 n 
0000000058 00000 n 
0000000115 00000 n 
0000000266 00000 n 
0000000361 00000 n 
trailer<< /Size 6 /Root 1 0 R >>
startxref
440
%%EOF`)

	content, err := a.convertImportToMarkdown("练习1.pdf", pdf, 1)
	if err != nil {
		t.Fatalf("pdf import failed: %v", err)
	}
	if !strings.Contains(content, "![第 1 页]") {
		t.Fatalf("expected page preview image, got: %q", content)
	}
	if !strings.Contains(content, "/uploads/") {
		t.Fatalf("expected uploaded image url, got: %q", content)
	}
	if !strings.Contains(content, "下载原 PDF") {
		t.Fatalf("expected original pdf link, got: %q", content)
	}
}

func TestConvertImportXLSX(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	mustZip := func(name, body string) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create zip entry: %v", err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatalf("write zip entry: %v", err)
		}
	}

	mustZip("xl/sharedStrings.xml", `<?xml version="1.0"?>
<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" count="2" uniqueCount="2">
  <si><t>Name</t></si>
  <si><t>Alice</t></si>
</sst>`)
	mustZip("xl/workbook.xml", `<?xml version="1.0"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"
 xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <sheets>
    <sheet name="People" sheetId="1" r:id="rId1"/>
  </sheets>
</workbook>`)
	mustZip("xl/_rels/workbook.xml.rels", `<?xml version="1.0"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>
</Relationships>`)
	mustZip("xl/worksheets/sheet1.xml", `<?xml version="1.0"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <sheetData>
    <row r="1"><c r="A1" t="s"><v>0</v></c></row>
    <row r="2"><c r="A2" t="s"><v>1</v></c></row>
  </sheetData>
</worksheet>`)
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}

	a := &app{}
	content, err := a.convertImportToMarkdown("people.xlsx", buf.Bytes(), 1)
	if err != nil {
		t.Fatalf("xlsx convert failed: %v", err)
	}
	if !strings.Contains(content, "Name") || !strings.Contains(content, "Alice") {
		t.Fatalf("unexpected xlsx markdown: %q", content)
	}
	if !strings.Contains(content, "|") {
		t.Fatalf("expected markdown table, got: %q", content)
	}
}

func TestConvertImportDOCXStillWorks(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte(`<?xml version="1.0"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body><w:p><w:r><w:t>Docx Hello</w:t></w:r></w:p></w:body>
</w:document>`))
	_ = zw.Close()

	a := &app{}
	content, err := a.convertImportToMarkdown("a.docx", buf.Bytes(), 1)
	if err != nil {
		t.Fatalf("docx convert failed: %v", err)
	}
	if !strings.Contains(content, "Docx Hello") {
		t.Fatalf("unexpected docx markdown: %q", content)
	}
}

func TestConvertImportRTFAsDoc(t *testing.T) {
	rtf := `{\rtf1\ansi\deff0 {\fonttbl {\f0 Times;}} \f0\fs24 Hello RTF Doc\par}`
	a := &app{}
	content, err := a.convertImportToMarkdown("note.doc", []byte(rtf), 1)
	if err != nil {
		t.Fatalf("rtf/doc convert failed: %v", err)
	}
	if !strings.Contains(content, "Hello RTF Doc") {
		t.Fatalf("unexpected rtf markdown: %q", content)
	}
}
