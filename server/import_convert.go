package main

import (
	"archive/zip"
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/ledongthuc/pdf"
)

const (
	importMaxExcelSheets = 30
	importMaxExcelRows   = 2000
	importMaxPDFPages    = 80
)

func (a *app) pdfImportToMarkdown(title, filename string, data []byte, userID int64) (string, error) {
	if len(data) < 5 || !bytes.HasPrefix(data, []byte("%PDF")) {
		return "", errors.New("不是有效的 PDF 文件")
	}

	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString("\n\n")

	pdfURL, pdfSaveErr := a.saveImportBytes(filename, data, "application/pdf", userID)
	if pdfSaveErr == nil {
		b.WriteString("[下载原 PDF](")
		b.WriteString(pdfURL)
		b.WriteString(")\n\n")
	}

	// 1) 纯 Go 文本层提取（可选中文字的 PDF）
	libText := sanitizePDFText(extractPDFPlainTextViaLibrary(data))
	if libText == "" {
		libText = sanitizePDFText(extractPDFText(data))
	}

	// 2) 可选增强：整页渲染 + OCR（扫描件/图片型 PDF → 可编辑文本）
	pages, renderedText, renderErr := a.renderPDFPages(data)
	if text := sanitizePDFText(renderedText); text != "" && len(text) >= len(libText) {
		libText = text
	}

	// 3) 若仍无文本，对已得到的页面图/内嵌图做 Tesseract OCR（Windows/Linux 常见）
	embedded := extractPDFEmbeddedImages(data)
	if libText == "" {
		ocrSources := pages
		if len(ocrSources) == 0 {
			ocrSources = embedded
		}
		if text := sanitizePDFText(ocrImagesWithTesseract(ocrSources)); text != "" {
			libText = text
		}
	}

	if libText != "" {
		b.WriteString("## 识别文本\n\n")
		b.WriteString("_以下文本可直接编辑；由 PDF 文本层或 OCR 识别得到，复杂版式可能有误差_\n\n")
		b.WriteString(libText)
		b.WriteString("\n\n")
	}

	if len(pages) > 0 {
		b.WriteString("## 页面预览\n\n")
		b.WriteString("_原页图片，便于对照版式；正文请优先编辑上方「识别文本」_\n\n")
		limit := len(pages)
		if limit > importMaxPDFPages {
			limit = importMaxPDFPages
		}
		for i := 0; i < limit; i++ {
			pageName := fmt.Sprintf("%s-第%d页.png", title, i+1)
			url, err := a.saveImportBytes(pageName, pages[i], "image/png", userID)
			if err != nil {
				continue
			}
			b.WriteString(fmt.Sprintf("![第 %d 页](%s)\n\n", i+1, url))
		}
		if len(pages) > importMaxPDFPages {
			b.WriteString(fmt.Sprintf("_提示：PDF 共 %d 页，仅导入前 %d 页预览_\n", len(pages), importMaxPDFPages))
		}
	} else if len(embedded) > 0 {
		b.WriteString("## 文档图片\n\n")
		b.WriteString("_已从 PDF 提取内嵌图片_\n\n")
		limit := len(embedded)
		if limit > importMaxPDFPages {
			limit = importMaxPDFPages
		}
		for i := 0; i < limit; i++ {
			ext := ".jpg"
			mimeType := "image/jpeg"
			if len(embedded[i]) >= 8 && bytes.Equal(embedded[i][:8], []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}) {
				ext = ".png"
				mimeType = "image/png"
			}
			name := fmt.Sprintf("%s-图%d%s", title, i+1, ext)
			url, err := a.saveImportBytes(name, embedded[i], mimeType, userID)
			if err != nil {
				continue
			}
			b.WriteString(fmt.Sprintf("![图片 %d](%s)\n\n", i+1, url))
		}
	}

	hasText := strings.Contains(b.String(), "## 识别文本")
	hasPages := strings.Contains(b.String(), "## 页面预览")
	hasImages := strings.Contains(b.String(), "## 文档图片")
	hasPDF := pdfSaveErr == nil && pdfURL != ""

	if !hasText && !hasPages && !hasImages {
		if hasPDF {
			b.WriteString("> 未能自动提取可读文本/图片（常见于扫描件）。可下载原 PDF，或安装 OCR 能力后重试：macOS 会用系统 Vision；Windows/Linux 可安装 Tesseract（`chi_sim`）。\n")
			if renderErr != nil {
				b.WriteString(">\n> 页面渲染不可用：")
				b.WriteString(renderErr.Error())
				b.WriteString("\n")
			}
			return strings.TrimSpace(b.String()) + "\n", nil
		}
		return "", errors.New("PDF 转换失败：未能提取文本/图片，且无法保存原文件")
	}

	if !hasText && (hasPages || hasImages) {
		b.WriteString("\n> 当前 PDF 更像图片型/扫描件，暂未识别出可编辑文字。macOS 会自动 OCR；Windows 可安装 [Tesseract](https://github.com/UB-Mannheim/tesseract/wiki)（含 `chi_sim`）后重新导入。\n")
	}
	return strings.TrimSpace(b.String()) + "\n", nil
}

func ocrImagesWithTesseract(images [][]byte) string {
	if len(images) == 0 {
		return ""
	}
	tess, err := exec.LookPath("tesseract")
	if err != nil {
		return ""
	}
	tmpDir, err := os.MkdirTemp("", "doc-pdf-ocr-*")
	if err != nil {
		return ""
	}
	defer os.RemoveAll(tmpDir)

	var parts []string
	limit := len(images)
	if limit > importMaxPDFPages {
		limit = importMaxPDFPages
	}
	for i := 0; i < limit; i++ {
		ext := ".jpg"
		if len(images[i]) >= 8 && bytes.Equal(images[i][:8], []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}) {
			ext = ".png"
		}
		imgPath := filepath.Join(tmpDir, fmt.Sprintf("page-%03d%s", i+1, ext))
		if err := os.WriteFile(imgPath, images[i], 0644); err != nil {
			continue
		}
		cmd := exec.Command(tess, imgPath, "stdout", "-l", "chi_sim+eng", "--psm", "6")
		out, err := cmd.CombinedOutput()
		if err != nil {
			// 兼容未安装中文语言包
			cmd = exec.Command(tess, imgPath, "stdout", "-l", "eng", "--psm", "6")
			out, err = cmd.CombinedOutput()
			if err != nil {
				continue
			}
		}
		text := strings.TrimSpace(string(out))
		if text == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("### 第 %d 页\n\n%s", i+1, text))
	}
	return strings.Join(parts, "\n\n")
}

func extractPDFPlainTextViaLibrary(data []byte) string {
	reader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return ""
	}
	plain, err := reader.GetPlainText()
	if err != nil {
		return ""
	}
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(plain); err != nil {
		return ""
	}
	return strings.TrimSpace(buf.String())
}

func extractPDFEmbeddedImages(data []byte) [][]byte {
	var images [][]byte
	seen := map[string]struct{}{}

	// 提取完整 JPEG（PDF 中很常见）
	for i := 0; i+3 < len(data); i++ {
		if data[i] != 0xFF || data[i+1] != 0xD8 || data[i+2] != 0xFF {
			continue
		}
		end := -1
		for j := i + 3; j+1 < len(data); j++ {
			if data[j] == 0xFF && data[j+1] == 0xD9 {
				end = j + 2
				break
			}
			// 防止跨过大对象误匹配
			if j-i > 12<<20 {
				break
			}
		}
		if end <= i+100 {
			continue
		}
		img := data[i:end]
		key := fmt.Sprintf("%d:%d", len(img), checksum4(img))
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		images = append(images, append([]byte(nil), img...))
		if len(images) >= importMaxPDFPages {
			break
		}
		i = end - 1
	}

	// 补充 PNG
	if len(images) < importMaxPDFPages {
		sig := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
		iend := []byte{0x49, 0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82}
		for i := 0; i+8 < len(data); i++ {
			if !bytes.Equal(data[i:i+8], sig) {
				continue
			}
			rel := bytes.Index(data[i+8:], iend)
			if rel < 0 || rel > 12<<20 {
				continue
			}
			end := i + 8 + rel + len(iend)
			img := data[i:end]
			key := fmt.Sprintf("png:%d:%d", len(img), checksum4(img))
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			images = append(images, append([]byte(nil), img...))
			if len(images) >= importMaxPDFPages {
				break
			}
			i = end - 1
		}
	}
	return images
}

func checksum4(data []byte) uint32 {
	var sum uint32
	step := len(data) / 64
	if step < 1 {
		step = 1
	}
	for i := 0; i < len(data); i += step {
		sum = sum*131 + uint32(data[i])
	}
	sum ^= uint32(len(data))
	return sum
}

func (a *app) renderPDFPages(data []byte) ([][]byte, string, error) {
	var errs []string

	if pages, text, err := a.renderPDFPagesWithPDFKit(data); err == nil && len(pages) > 0 {
		return pages, text, nil
	} else if err != nil {
		errs = append(errs, err.Error())
	}

	if pages, err := a.renderPDFPagesWithGhostscript(data); err == nil && len(pages) > 0 {
		return pages, "", nil
	} else if err != nil {
		errs = append(errs, err.Error())
	}

	if len(errs) == 0 {
		return nil, "", errors.New("未能渲染 PDF 页面")
	}
	return nil, "", errors.New(strings.Join(errs, "；"))
}

func (a *app) renderPDFPagesWithPDFKit(data []byte) ([][]byte, string, error) {
	helper, err := a.ensurePDFRenderHelper()
	if err != nil {
		return nil, "", err
	}

	tmpDir, err := os.MkdirTemp("", "doc-pdf-pdfkit-*")
	if err != nil {
		return nil, "", err
	}
	defer os.RemoveAll(tmpDir)

	pdfPath := filepath.Join(tmpDir, "input.pdf")
	outDir := filepath.Join(tmpDir, "out")
	if err := os.WriteFile(pdfPath, data, 0644); err != nil {
		return nil, "", err
	}
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return nil, "", err
	}

	cmd := exec.Command(helper, pdfPath, outDir, strconv.Itoa(importMaxPDFPages))
	output, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(output))
		if msg == "" {
			msg = err.Error()
		}
		return nil, "", fmt.Errorf("PDFKit 渲染失败：%s", msg)
	}

	matches, err := filepath.Glob(filepath.Join(outDir, "page-*.png"))
	if err != nil {
		return nil, "", err
	}
	sort.Strings(matches)
	pages := make([][]byte, 0, len(matches))
	for _, match := range matches {
		img, err := os.ReadFile(match)
		if err != nil || len(img) == 0 {
			continue
		}
		pages = append(pages, img)
	}
	textBytes, _ := os.ReadFile(filepath.Join(outDir, "text.txt"))
	text := string(textBytes)
	if len(pages) == 0 {
		return nil, text, errors.New("PDFKit 未输出页面图片")
	}
	return pages, text, nil
}

func (a *app) ensurePDFRenderHelper() (string, error) {
	if runtime.GOOS != "darwin" {
		return "", errors.New("当前系统不是 macOS，无法使用 PDFKit")
	}
	source := findPDFRenderSource()
	if source == "" {
		return "", errors.New("未找到 tools/pdf_render.swift")
	}
	srcInfo, err := os.Stat(source)
	if err != nil {
		return "", err
	}

	outPath := ""
	if a != nil && a.dataDir != "" {
		outPath = filepath.Join(a.dataDir, "bin", "pdf_render")
	} else if exe, err := os.Executable(); err == nil {
		outPath = filepath.Join(filepath.Dir(exe), "pdf_render")
	} else {
		outPath = filepath.Join(os.TempDir(), "doc-system-pdf_render")
	}

	if st, err := os.Stat(outPath); err == nil && !st.IsDir() && st.Mode()&0111 != 0 && !srcInfo.ModTime().After(st.ModTime()) {
		return outPath, nil
	}

	swiftc, err := exec.LookPath("swiftc")
	if err != nil {
		return "", errors.New("未找到 swiftc，无法编译 PDF 渲染工具")
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return "", err
	}

	cmd := exec.Command(
		swiftc,
		"-O",
		"-o", outPath,
		source,
		"-framework", "PDFKit",
		"-framework", "AppKit",
		"-framework", "Vision",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(output))
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("编译 PDF 渲染工具失败：%s", msg)
	}
	return outPath, nil
}

func findPDFRenderSource() string {
	candidates := []string{
		filepath.Join("tools", "pdf_render.swift"),
	}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates,
			filepath.Join(filepath.Dir(exe), "tools", "pdf_render.swift"),
			filepath.Join(filepath.Dir(exe), "..", "tools", "pdf_render.swift"),
		)
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(wd, "tools", "pdf_render.swift"),
			filepath.Join(wd, "server", "tools", "pdf_render.swift"),
		)
	}
	for _, candidate := range candidates {
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return candidate
		}
	}
	return ""
}

func (a *app) renderPDFPagesWithGhostscript(data []byte) ([][]byte, error) {
	gs := findGhostscript()
	if gs == "" {
		return nil, errors.New("未找到 Ghostscript（gs）")
	}

	tmpDir, err := os.MkdirTemp("", "doc-pdf-gs-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	pdfPath := filepath.Join(tmpDir, "input.pdf")
	if err := os.WriteFile(pdfPath, data, 0644); err != nil {
		return nil, err
	}

	outPattern := filepath.Join(tmpDir, "page-%03d.png")
	cmd := exec.Command(
		gs,
		"-dSAFER",
		"-dBATCH",
		"-dNOPAUSE",
		"-dQUIET",
		"-sDEVICE=png16m",
		"-r144",
		"-dTextAlphaBits=4",
		"-dGraphicsAlphaBits=4",
		"-dFirstPage=1",
		fmt.Sprintf("-dLastPage=%d", importMaxPDFPages),
		"-sOutputFile="+outPattern,
		pdfPath,
	)
	if lib := guessGhostscriptLib(); lib != "" {
		cmd.Env = append(os.Environ(), "GS_LIB="+lib)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(output))
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("Ghostscript 渲染失败：%s", msg)
	}

	matches, err := filepath.Glob(filepath.Join(tmpDir, "page-*.png"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	if len(matches) == 0 {
		return nil, errors.New("Ghostscript 未输出任何页面图片")
	}

	pages := make([][]byte, 0, len(matches))
	for _, match := range matches {
		img, err := os.ReadFile(match)
		if err != nil || len(img) == 0 {
			continue
		}
		pages = append(pages, img)
	}
	if len(pages) == 0 {
		return nil, errors.New("页面图片读取失败")
	}
	return pages, nil
}

func (a *app) saveImportBytes(originalName string, data []byte, mimeType string, userID int64) (string, error) {
	now := time.Now()
	dir := filepath.Join(a.uploadDir, now.Format("2006"), now.Format("01"))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	ext := strings.ToLower(filepath.Ext(originalName))
	if ext == "" {
		switch mimeType {
		case "image/png":
			ext = ".png"
		case "application/pdf":
			ext = ".pdf"
		}
	}
	name := now.Format("20060102150405") + "_" + mustToken(4) + ext
	target := filepath.Join(dir, name)
	if err := os.WriteFile(target, data, 0644); err != nil {
		return "", err
	}
	rel := filepath.ToSlash(filepath.Join(now.Format("2006"), now.Format("01"), name))
	_, _ = a.db.Exec(
		`INSERT INTO attachments (original_name, file_path, mime_type, file_size, created_by, created_at)
		 VALUES (?, ?, ?, ?, ?, datetime('now'))`,
		originalName, rel, mimeType, len(data), userID,
	)
	return "/uploads/" + rel, nil
}

func findGhostscript() string {
	names := []string{"gs", "gswin64c", "gswin32c"}
	for _, name := range names {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}

	candidates := []string{
		"/Applications/ServBay/bin/gs",
		"/opt/homebrew/bin/gs",
		"/usr/local/bin/gs",
		"/usr/bin/gs",
		`C:\Program Files\gs\gs10.04.0\bin\gswin64c.exe`,
		`C:\Program Files\gs\gs10.03.1\bin\gswin64c.exe`,
		`C:\Program Files\gs\gs10.02.1\bin\gswin64c.exe`,
		`C:\Program Files\gs\gs10.01.2\bin\gswin64c.exe`,
		`C:\Program Files (x86)\gs\gs10.04.0\bin\gswin32c.exe`,
	}
	// 扫描常见 Ghostscript 安装目录（版本号不固定）
	for _, root := range []string{`C:\Program Files\gs`, `C:\Program Files (x86)\gs`} {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			for _, exeName := range []string{"gswin64c.exe", "gswin32c.exe", "gs.exe"} {
				candidates = append(candidates, filepath.Join(root, entry.Name(), "bin", exeName))
			}
		}
	}

	for _, candidate := range candidates {
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return candidate
		}
	}
	return ""
}

func guessGhostscriptLib() string {
	candidates := []string{
		"/Applications/ServBay/package/common/share/ghostscript/10.02.1/Resource/Init",
		"/opt/homebrew/share/ghostscript/Resource/Init",
		"/usr/local/share/ghostscript/Resource/Init",
	}
	var parts []string
	for _, candidate := range candidates {
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			parts = append(parts, candidate)
			lib := filepath.Join(filepath.Dir(filepath.Dir(candidate)), "lib")
			if st, err := os.Stat(lib); err == nil && st.IsDir() {
				parts = append(parts, lib)
			}
		}
	}
	return strings.Join(parts, ":")
}

func sanitizePDFText(text string) string {
	text = compactBlankLines(text)
	text = strings.TrimSpace(text)
	if text == "" || isGarbledPDFText(text) {
		return ""
	}
	return text
}

func isGarbledPDFText(text string) bool {
	runes := []rune(text)
	if len(runes) == 0 {
		return true
	}
	good := 0
	bad := 0
	for _, r := range runes {
		switch {
		case r == unicode.ReplacementChar:
			bad += 3
		case r >= 0xE000 && r <= 0xF8FF: // PUA
			bad += 2
		case r >= 0xF0000 && r <= 0xFFFFD:
			bad += 2
		case unicode.IsControl(r) && r != '\n' && r != '\t' && r != '\r':
			bad++
		case unicode.Is(unicode.Han, r) || unicode.IsLetter(r) || unicode.IsDigit(r):
			good++
		case unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r):
			// neutral
		default:
			// 私有区/罕见符号常见于 CID 字体原文
			if r > 0x2FFF {
				bad++
			}
		}
	}
	if good < 12 {
		return true
	}
	return bad*100/(good+bad+1) >= 25
}

func extractPDFText(data []byte) string {
	var parts []string
	streamRe := regexp.MustCompile(`(?s)stream\r?\n(.*?)\r?\nendstream`)
	matches := streamRe.FindAllSubmatch(data, -1)
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		raw := match[1]
		decoded := tryPDFFlate(raw)
		if decoded == nil {
			decoded = raw
		}
		// 只处理看起来像页面内容流的数据（包含文本绘图操作符）
		if !bytes.Contains(decoded, []byte(" Tj")) &&
			!bytes.Contains(decoded, []byte(" TJ")) &&
			!bytes.Contains(decoded, []byte("Tj")) &&
			!bytes.Contains(decoded, []byte("'")) &&
			!bytes.Contains(decoded, []byte("BT")) {
			continue
		}
		pageText := extractPDFOperatorsText(decoded)
		if strings.TrimSpace(pageText) != "" {
			parts = append(parts, pageText)
		}
		if len(parts) >= importMaxPDFPages {
			break
		}
	}
	return strings.Join(parts, "\n\n")
}

func tryPDFFlate(raw []byte) []byte {
	candidates := [][]byte{raw}
	if len(raw) > 0 && raw[0] == '\n' {
		candidates = append(candidates, raw[1:])
	}
	if len(raw) > 1 && raw[0] == '\r' && raw[1] == '\n' {
		candidates = append(candidates, raw[2:])
	}
	for _, candidate := range candidates {
		reader, err := zlib.NewReader(bytes.NewReader(candidate))
		if err != nil {
			continue
		}
		out, err := io.ReadAll(reader)
		reader.Close()
		if err == nil && len(out) > 0 {
			return out
		}
	}
	return nil
}

func extractPDFOperatorsText(content []byte) string {
	var b strings.Builder
	i := 0
	for i < len(content) {
		switch content[i] {
		case '(':
			str, next, ok := readPDFLiteralString(content, i)
			if !ok {
				i++
				continue
			}
			b.WriteString(str)
			i = next
		case '[':
			end := i + 1
			depth := 1
			for end < len(content) && depth > 0 {
				if content[end] == '(' {
					str, next, ok := readPDFLiteralString(content, end)
					if ok {
						b.WriteString(str)
						end = next
						continue
					}
				}
				if content[end] == '[' {
					depth++
				} else if content[end] == ']' {
					depth--
				}
				end++
			}
			i = end
		case '<':
			if i+1 < len(content) && content[i+1] == '<' {
				i += 2
				continue
			}
			str, next, ok := readPDFHexString(content, i)
			if ok {
				b.WriteString(str)
				i = next
				continue
			}
			i++
		case '\'', '"':
			b.WriteByte('\n')
			i++
		default:
			i++
		}
	}
	text := b.String()
	text = regexp.MustCompile(`[^\S\n]{2,}`).ReplaceAllString(text, " ")
	return strings.TrimSpace(text)
}

func readPDFLiteralString(data []byte, start int) (string, int, bool) {
	if start >= len(data) || data[start] != '(' {
		return "", start, false
	}
	var b strings.Builder
	i := start + 1
	depth := 1
	for i < len(data) {
		ch := data[i]
		if ch == '\\' && i+1 < len(data) {
			next := data[i+1]
			switch next {
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			case '(', ')', '\\':
				b.WriteByte(next)
			case '\n':
			case '\r':
				if i+2 < len(data) && data[i+2] == '\n' {
					i++
				}
			default:
				if next >= '0' && next <= '7' {
					val := int(next - '0')
					consumed := 1
					for consumed < 3 && i+1+consumed < len(data) {
						d := data[i+1+consumed]
						if d < '0' || d > '7' {
							break
						}
						val = val*8 + int(d-'0')
						consumed++
					}
					b.WriteByte(byte(val))
					i += consumed
					continue
				}
				b.WriteByte(next)
			}
			i += 2
			continue
		}
		if ch == '(' {
			depth++
			b.WriteByte(ch)
			i++
			continue
		}
		if ch == ')' {
			depth--
			if depth == 0 {
				return b.String(), i + 1, true
			}
			b.WriteByte(ch)
			i++
			continue
		}
		b.WriteByte(ch)
		i++
	}
	return "", start, false
}

func readPDFHexString(data []byte, start int) (string, int, bool) {
	if start >= len(data) || data[start] != '<' {
		return "", start, false
	}
	end := bytes.IndexByte(data[start+1:], '>')
	if end < 0 {
		return "", start, false
	}
	end = start + 1 + end
	hexDigits := make([]byte, 0, end-(start+1))
	for _, ch := range data[start+1 : end] {
		if (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F') {
			hexDigits = append(hexDigits, ch)
		}
	}
	if len(hexDigits)%2 == 1 {
		hexDigits = append(hexDigits, '0')
	}
	out := make([]byte, 0, len(hexDigits)/2)
	for i := 0; i+1 < len(hexDigits); i += 2 {
		v, err := strconv.ParseUint(string(hexDigits[i:i+2]), 16, 8)
		if err != nil {
			continue
		}
		out = append(out, byte(v))
	}
	if isMostlyPrintable(out) {
		return string(out), end + 1, true
	}
	if len(out) >= 2 {
		u16 := make([]uint16, 0, len(out)/2)
		i := 0
		if out[0] == 0xfe && out[1] == 0xff {
			i = 2
		}
		for i+1 < len(out) {
			u16 = append(u16, binary.BigEndian.Uint16(out[i:i+2]))
			i += 2
		}
		text := string(utf16.Decode(u16))
		if strings.TrimSpace(text) != "" {
			return text, end + 1, true
		}
	}
	return "", end + 1, true
}

func excelToMarkdown(filename string, data []byte) (string, error) {
	ext := strings.ToLower(path.Ext(filename))
	switch ext {
	case ".xlsx":
		return xlsxToMarkdown(data)
	case ".xls":
		return xlsToMarkdown(data)
	default:
		return "", errors.New("不支持的 Excel 格式")
	}
}

func xlsxToMarkdown(data []byte) (string, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", errors.New("XLSX 文件解析失败，请确认文件未损坏")
	}

	sharedStrings, err := readXLSXSharedStrings(reader)
	if err != nil {
		return "", err
	}
	sheets, err := readXLSXSheetList(reader)
	if err != nil {
		return "", err
	}
	if len(sheets) == 0 {
		return "", errors.New("XLSX 文件中没有工作表")
	}

	var b strings.Builder
	limit := len(sheets)
	if limit > importMaxExcelSheets {
		limit = importMaxExcelSheets
	}
	for i := 0; i < limit; i++ {
		sheet := sheets[i]
		rows, err := readXLSXSheetRows(reader, sheet.path, sharedStrings)
		if err != nil {
			return "", err
		}
		table := rowsToMarkdownTable(rows)
		if strings.TrimSpace(table) == "" {
			continue
		}
		if len(sheets) > 1 {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString("## ")
			b.WriteString(sheet.name)
			b.WriteString("\n\n")
		}
		b.WriteString(table)
		if !strings.HasSuffix(table, "\n") {
			b.WriteString("\n")
		}
	}
	if b.Len() == 0 {
		return "", errors.New("XLSX 文件没有可导入的单元格内容")
	}
	if len(sheets) > importMaxExcelSheets {
		b.WriteString("\n_提示：仅导入前 ")
		b.WriteString(strconv.Itoa(importMaxExcelSheets))
		b.WriteString(" 个工作表_\n")
	}
	return b.String(), nil
}

type xlsxSheet struct {
	name string
	path string
}

func readXLSXSharedStrings(reader *zip.Reader) ([]string, error) {
	file := findZipFile(reader, "xl/sharedStrings.xml")
	if file == nil {
		return nil, nil
	}
	rc, err := file.Open()
	if err != nil {
		return nil, errors.New("读取 XLSX 共享字符串失败")
	}
	defer rc.Close()

	type tNode struct {
		Text string `xml:",chardata"`
	}
	type siNode struct {
		T   *tNode  `xml:"t"`
		RTs []tNode `xml:"r>t"`
		Raw string  `xml:",innerxml"`
	}
	type sst struct {
		SI []siNode `xml:"si"`
	}
	var doc sst
	if err := xml.NewDecoder(rc).Decode(&doc); err != nil {
		return nil, errors.New("解析 XLSX 共享字符串失败")
	}
	out := make([]string, 0, len(doc.SI))
	for _, si := range doc.SI {
		if si.T != nil {
			out = append(out, si.T.Text)
			continue
		}
		if len(si.RTs) > 0 {
			var b strings.Builder
			for _, part := range si.RTs {
				b.WriteString(part.Text)
			}
			out = append(out, b.String())
			continue
		}
		text := regexp.MustCompile(`(?s)<[^>]+>`).ReplaceAllString(si.Raw, "")
		out = append(out, text)
	}
	return out, nil
}

func readXLSXSheetList(reader *zip.Reader) ([]xlsxSheet, error) {
	relMap := map[string]string{}
	if rels := findZipFile(reader, "xl/_rels/workbook.xml.rels"); rels != nil {
		rc, err := rels.Open()
		if err == nil {
			type relationship struct {
				ID     string `xml:"Id,attr"`
				Target string `xml:"Target,attr"`
			}
			type relationships struct {
				Relationship []relationship `xml:"Relationship"`
			}
			var doc relationships
			if err := xml.NewDecoder(rc).Decode(&doc); err == nil {
				for _, item := range doc.Relationship {
					target := strings.ReplaceAll(item.Target, "\\", "/")
					target = strings.TrimPrefix(target, "/")
					if !strings.HasPrefix(target, "xl/") {
						target = path.Join("xl", target)
					}
					relMap[item.ID] = path.Clean(target)
				}
			}
			rc.Close()
		}
	}

	wb := findZipFile(reader, "xl/workbook.xml")
	if wb == nil {
		return nil, errors.New("XLSX 缺少 workbook.xml")
	}
	rc, err := wb.Open()
	if err != nil {
		return nil, errors.New("读取 XLSX workbook 失败")
	}
	defer rc.Close()

	var sheets []xlsxSheet
	decoder := xml.NewDecoder(rc)
	sheetIndex := 0
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, errors.New("解析 XLSX workbook 失败")
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "sheet" {
			continue
		}
		sheetIndex++
		name := ""
		relID := ""
		for _, attr := range start.Attr {
			switch {
			case attr.Name.Local == "name":
				name = attr.Value
			case attr.Name.Local == "id":
				relID = attr.Value
			}
		}
		sheetPath := relMap[relID]
		if sheetPath == "" {
			sheetPath = fmt.Sprintf("xl/worksheets/sheet%d.xml", sheetIndex)
		}
		if strings.TrimSpace(name) == "" {
			name = fmt.Sprintf("Sheet%d", sheetIndex)
		}
		sheets = append(sheets, xlsxSheet{name: name, path: sheetPath})
	}
	return sheets, nil
}

func readXLSXSheetRows(reader *zip.Reader, sheetPath string, shared []string) ([][]string, error) {
	file := findZipFile(reader, sheetPath)
	if file == nil && !strings.HasPrefix(sheetPath, "xl/") {
		file = findZipFile(reader, path.Join("xl", sheetPath))
	}
	if file == nil {
		return nil, fmt.Errorf("找不到工作表文件：%s", sheetPath)
	}
	rc, err := file.Open()
	if err != nil {
		return nil, errors.New("读取 XLSX 工作表失败")
	}
	defer rc.Close()

	type cellNode struct {
		Ref  string `xml:"r,attr"`
		Type string `xml:"t,attr"`
		V    string `xml:"v"`
		Is   struct {
			T string `xml:"t"`
		} `xml:"is"`
	}
	type rowNode struct {
		Cells []cellNode `xml:"c"`
	}
	type worksheet struct {
		SheetData struct {
			Rows []rowNode `xml:"row"`
		} `xml:"sheetData"`
	}
	var doc worksheet
	if err := xml.NewDecoder(rc).Decode(&doc); err != nil {
		return nil, errors.New("解析 XLSX 工作表失败")
	}

	var rows [][]string
	for _, row := range doc.SheetData.Rows {
		if len(rows) >= importMaxExcelRows {
			break
		}
		maxCol := 0
		values := map[int]string{}
		for _, cell := range row.Cells {
			col := excelColIndex(cell.Ref)
			if col < 0 {
				continue
			}
			if col > maxCol {
				maxCol = col
			}
			values[col] = xlsxCellText(cell.Type, cell.V, cell.Is.T, shared)
		}
		line := make([]string, maxCol+1)
		for col, value := range values {
			line[col] = value
		}
		rows = append(rows, line)
	}
	return rows, nil
}

func xlsxCellText(cellType, value, inline string, shared []string) string {
	switch cellType {
	case "s":
		idx, err := strconv.Atoi(strings.TrimSpace(value))
		if err == nil && idx >= 0 && idx < len(shared) {
			return shared[idx]
		}
		return value
	case "inlineStr":
		return inline
	default:
		return value
	}
}

func excelColIndex(cellRef string) int {
	if cellRef == "" {
		return -1
	}
	col := 0
	for i := 0; i < len(cellRef); i++ {
		ch := cellRef[i]
		if ch < 'A' || ch > 'Z' {
			if i == 0 {
				return -1
			}
			return col - 1
		}
		col = col*26 + int(ch-'A'+1)
	}
	return col - 1
}

func findZipFile(reader *zip.Reader, name string) *zip.File {
	name = strings.TrimPrefix(strings.ReplaceAll(name, "\\", "/"), "/")
	for _, file := range reader.File {
		if file.Name == name {
			return file
		}
	}
	return nil
}

func rowsToMarkdownTable(rows [][]string) string {
	if len(rows) == 0 {
		return ""
	}
	width := 0
	for _, row := range rows {
		if len(row) > width {
			width = len(row)
		}
	}
	if width == 0 {
		return ""
	}
	for len(rows) > 0 {
		last := rows[len(rows)-1]
		empty := true
		for _, cell := range last {
			if strings.TrimSpace(cell) != "" {
				empty = false
				break
			}
		}
		if !empty {
			break
		}
		rows = rows[:len(rows)-1]
	}
	if len(rows) == 0 {
		return ""
	}

	var b strings.Builder
	writeRow := func(row []string) {
		b.WriteString("|")
		for i := 0; i < width; i++ {
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			b.WriteString(" ")
			b.WriteString(escapeMarkdownTableCell(cell))
			b.WriteString(" |")
		}
		b.WriteString("\n")
	}
	writeRow(rows[0])
	b.WriteString("|")
	for i := 0; i < width; i++ {
		b.WriteString(" --- |")
	}
	b.WriteString("\n")
	for _, row := range rows[1:] {
		writeRow(row)
	}
	return b.String()
}

func xlsToMarkdown(data []byte) (string, error) {
	trimmed := bytes.TrimSpace(data)
	head := trimmed
	if len(head) > 256 {
		head = head[:256]
	}
	if bytes.HasPrefix(trimmed, []byte("<?xml")) || bytes.Contains(head, []byte("Spreadsheet")) {
		text := htmlToMarkdownText(string(data))
		if strings.TrimSpace(text) == "" {
			return "", errors.New("XML Spreadsheet 内容为空")
		}
		return text, nil
	}
	if bytes.HasPrefix(bytes.ToLower(head), []byte("<html")) || bytes.Contains(bytes.ToLower(head), []byte("<table")) {
		text := htmlToMarkdownText(string(data))
		if strings.TrimSpace(text) == "" {
			return "", errors.New("HTML 表格内容为空")
		}
		return text, nil
	}
	if len(data) < 8 || data[0] != 0xD0 || data[1] != 0xCF {
		return "", errors.New("无法识别的 .xls 文件，请另存为 .xlsx 或 .csv 后再导入")
	}

	rows := extractXLSBIFFRows(data)
	table := rowsToMarkdownTable(rows)
	if strings.TrimSpace(table) == "" {
		fallback := extractPrintableRuns(data, 6)
		if strings.TrimSpace(fallback) == "" {
			return "", errors.New("未能从 .xls 提取表格内容，请另存为 .xlsx 或 .csv 后再导入")
		}
		return fallback, nil
	}
	return table, nil
}

func extractXLSBIFFRows(data []byte) [][]string {
	sst := []string{}
	type cell struct {
		row, col int
		value    string
	}
	var cells []cell

	i := 0
	for i+4 <= len(data) {
		recType := binary.LittleEndian.Uint16(data[i : i+2])
		recSize := int(binary.LittleEndian.Uint16(data[i+2 : i+4]))
		i += 4
		if recSize < 0 || i+recSize > len(data) {
			break
		}
		payload := data[i : i+recSize]
		i += recSize

		switch recType {
		case 0x00FC:
			sst = parseXlsSST(payload)
		case 0x00FD:
			if len(payload) < 10 {
				continue
			}
			row := int(binary.LittleEndian.Uint16(payload[0:2]))
			col := int(binary.LittleEndian.Uint16(payload[2:4]))
			idx := int(binary.LittleEndian.Uint32(payload[6:10]))
			value := ""
			if idx >= 0 && idx < len(sst) {
				value = sst[idx]
			}
			cells = append(cells, cell{row: row, col: col, value: value})
		case 0x0204:
			if len(payload) < 8 {
				continue
			}
			row := int(binary.LittleEndian.Uint16(payload[0:2]))
			col := int(binary.LittleEndian.Uint16(payload[2:4]))
			strLen := int(binary.LittleEndian.Uint16(payload[6:8]))
			value := decodeXlsLabelString(payload[8:], strLen)
			cells = append(cells, cell{row: row, col: col, value: value})
		}
	}

	if len(cells) == 0 {
		return nil
	}
	maxRow := 0
	maxCol := 0
	for _, c := range cells {
		if c.row > maxRow {
			maxRow = c.row
		}
		if c.col > maxCol {
			maxCol = c.col
		}
	}
	if maxRow+1 > importMaxExcelRows {
		maxRow = importMaxExcelRows - 1
	}
	rows := make([][]string, maxRow+1)
	for r := range rows {
		rows[r] = make([]string, maxCol+1)
	}
	for _, c := range cells {
		if c.row > maxRow || c.col > maxCol {
			continue
		}
		rows[c.row][c.col] = c.value
	}
	return rows
}

func parseXlsSST(payload []byte) []string {
	if len(payload) < 8 {
		return nil
	}
	count := int(binary.LittleEndian.Uint32(payload[4:8]))
	pos := 8
	out := make([]string, 0, count)
	for len(out) < count && pos < len(payload) {
		str, next := readXlsXLUnicodeRichExtendedString(payload, pos)
		if next <= pos {
			break
		}
		out = append(out, str)
		pos = next
	}
	return out
}

func readXlsXLUnicodeRichExtendedString(data []byte, pos int) (string, int) {
	if pos+3 > len(data) {
		return "", pos
	}
	charCount := int(binary.LittleEndian.Uint16(data[pos : pos+2]))
	flags := data[pos+2]
	pos += 3
	richCount := 0
	if flags&0x08 != 0 {
		if pos+2 > len(data) {
			return "", pos
		}
		richCount = int(binary.LittleEndian.Uint16(data[pos : pos+2]))
		pos += 2
	}
	extSize := 0
	if flags&0x04 != 0 {
		if pos+4 > len(data) {
			return "", pos
		}
		extSize = int(binary.LittleEndian.Uint32(data[pos : pos+4]))
		pos += 4
	}
	str, next := decodeXlsStringAt(data, pos, charCount, flags&0x01 != 0)
	pos = next
	pos += richCount * 4
	pos += extSize
	if pos > len(data) {
		pos = len(data)
	}
	return str, pos
}

func decodeXlsLabelString(payload []byte, charCount int) string {
	if len(payload) == 0 || charCount <= 0 {
		return ""
	}
	unicode := payload[0]&0x01 != 0
	str, _ := decodeXlsStringAt(payload, 1, charCount, unicode)
	return str
}

func decodeXlsStringAt(data []byte, pos, charCount int, unicode bool) (string, int) {
	if charCount <= 0 {
		return "", pos
	}
	if unicode {
		need := charCount * 2
		if pos+need > len(data) {
			need = len(data) - pos
			charCount = need / 2
		}
		u16 := make([]uint16, charCount)
		for i := 0; i < charCount; i++ {
			u16[i] = binary.LittleEndian.Uint16(data[pos+i*2 : pos+i*2+2])
		}
		return string(utf16.Decode(u16)), pos + charCount*2
	}
	if pos+charCount > len(data) {
		charCount = len(data) - pos
	}
	b := make([]rune, charCount)
	for i := 0; i < charCount; i++ {
		b[i] = rune(data[pos+i])
	}
	return string(b), pos + charCount
}

func docLegacyToMarkdown(data []byte) (string, error) {
	trimmed := bytes.TrimSpace(data)
	if bytes.HasPrefix(trimmed, []byte(`{\rtf`)) {
		text := compactBlankLines(rtfToText(string(trimmed)))
		if strings.TrimSpace(text) == "" {
			return "", errors.New("RTF 文档没有可导入的文本内容")
		}
		return text, nil
	}
	if bytes.HasPrefix(trimmed, []byte("PK")) {
		return docxToMarkdown(data)
	}
	if len(data) < 8 || data[0] != 0xD0 || data[1] != 0xCF || data[2] != 0x11 || data[3] != 0xE0 {
		return "", errors.New("不是有效的旧版 .doc 文件，请另存为 .docx 后再导入")
	}

	text := compactBlankLines(extractDocOLEText(data))
	if strings.TrimSpace(text) == "" {
		return "", errors.New("未能从旧版 .doc 提取到文本，请用 Word/WPS 另存为 .docx 后再导入")
	}
	return text, nil
}

func extractDocOLEText(data []byte) string {
	var pieces []string
	for i := 0; i+1 < len(data); {
		if data[i] == 0 && data[i+1] == 0 {
			i += 2
			continue
		}
		j := i
		u16 := []uint16{}
		for j+1 < len(data) {
			code := binary.LittleEndian.Uint16(data[j : j+2])
			if code == 0 {
				break
			}
			r := rune(code)
			if code >= 0xD800 && code <= 0xDFFF {
				break
			}
			if !isDocUsefulRune(r) {
				break
			}
			u16 = append(u16, code)
			j += 2
			if len(u16) > 4000 {
				break
			}
		}
		if len(u16) >= 4 {
			s := strings.TrimSpace(string(utf16.Decode(u16)))
			if s != "" && looksLikeDocumentText(s) {
				pieces = append(pieces, s)
			}
			i = j
			continue
		}
		i++
	}

	ascii := extractPrintableRuns(data, 10)
	if ascii != "" {
		pieces = append(pieces, ascii)
	}
	if len(pieces) == 0 {
		return ""
	}
	return strings.Join(dedupeKeepOrder(pieces), "\n\n")
}

func isDocUsefulRune(r rune) bool {
	if r == '\n' || r == '\r' || r == '\t' {
		return true
	}
	if r < 32 {
		return false
	}
	return unicode.IsPrint(r)
}

func looksLikeDocumentText(s string) bool {
	letters := 0
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.In(r, unicode.Han) {
			letters++
		}
	}
	return letters >= 3
}

func rtfToText(raw string) string {
	var b strings.Builder
	inControl := false
	var control strings.Builder
	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		if inControl {
			if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '-' || (ch >= '0' && ch <= '9') {
				control.WriteByte(ch)
				continue
			}
			name := control.String()
			control.Reset()
			inControl = false
			switch {
			case name == "par" || name == "line":
				b.WriteByte('\n')
			case name == "tab":
				b.WriteByte('\t')
			case strings.HasPrefix(name, "u") && len(name) > 1:
				num := strings.TrimPrefix(name, "u")
				negative := strings.HasPrefix(num, "-")
				num = strings.TrimPrefix(num, "-")
				if v, err := strconv.Atoi(num); err == nil {
					if negative {
						v = -v
					}
					b.WriteRune(rune(v))
				}
			}
			if ch == ' ' {
				continue
			}
		}
		switch ch {
		case '\\':
			if i+1 < len(raw) {
				next := raw[i+1]
				if next == '\\' || next == '{' || next == '}' {
					b.WriteByte(next)
					i++
					continue
				}
				if next == '\'' && i+3 < len(raw) {
					hex := raw[i+2 : i+4]
					if v, err := strconv.ParseUint(hex, 16, 8); err == nil {
						b.WriteByte(byte(v))
						i += 3
						continue
					}
				}
			}
			inControl = true
			control.Reset()
		case '{', '}', '\r', '\n':
		default:
			b.WriteByte(ch)
		}
	}
	return b.String()
}

func extractPrintableRuns(data []byte, minLen int) string {
	var b strings.Builder
	var run strings.Builder
	flush := func() {
		s := strings.TrimSpace(run.String())
		run.Reset()
		if len([]rune(s)) >= minLen && looksLikeDocumentText(s) {
			if b.Len() > 0 {
				b.WriteString("\n\n")
			}
			b.WriteString(s)
		}
	}
	for i := 0; i < len(data); {
		r, size := utf8.DecodeRune(data[i:])
		if r == utf8.RuneError && size == 1 {
			ch := data[i]
			if ch >= 32 && ch < 127 {
				run.WriteByte(ch)
			} else if ch == '\n' || ch == '\r' || ch == '\t' {
				run.WriteByte(' ')
			} else {
				flush()
			}
			i++
			continue
		}
		if unicode.IsPrint(r) || r == '\n' || r == '\t' {
			if r == '\n' {
				run.WriteByte('\n')
			} else {
				run.WriteRune(r)
			}
		} else {
			flush()
		}
		i += size
	}
	flush()
	return b.String()
}

func isMostlyPrintable(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	printable := 0
	for _, ch := range data {
		if ch == 9 || ch == 10 || ch == 13 || (ch >= 32 && ch < 127) {
			printable++
		}
	}
	return printable*100/len(data) >= 80
}

func dedupeKeepOrder(items []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		key := strings.TrimSpace(item)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}
