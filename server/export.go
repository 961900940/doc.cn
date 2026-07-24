package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type exportStats struct {
	AppName          string `json:"app_name"`
	ExportedAt       string `json:"exported_at"`
	FolderCount      int    `json:"folder_count"`
	DocumentCount    int    `json:"document_count"`
	UploadCount      int    `json:"upload_count"`
	MissingDocuments int    `json:"missing_documents"`
}

func (a *app) handleExport(w http.ResponseWriter, r *http.Request, user User) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/export/documents/") {
		a.handleDocumentExport(w, r, user)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/export/folders/") {
		a.handleFolderExport(w, r, user)
		return
	}
	if r.URL.Path != "/api/export" {
		notFound(w)
		return
	}
	if !isAdmin(user) {
		http.Error(w, "只有管理员可以导出知识库", http.StatusForbidden)
		return
	}

	file, err := os.CreateTemp("", "doc-system-export-*.zip")
	if err != nil {
		serverError(w, err)
		return
	}
	defer os.Remove(file.Name())
	defer file.Close()

	stats, err := a.writeKnowledgeBaseExport(file)
	if err != nil {
		serverError(w, err)
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		serverError(w, err)
		return
	}

	filename := fmt.Sprintf("doc-system-export-%s.zip", time.Now().Format("20060102-150405"))
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	a.addOperationLog(user.ID, "export.knowledge_base", "export", 0,
		fmt.Sprintf("导出知识库 ZIP：%d 个文件夹、%d 篇文档、%d 个附件", stats.FolderCount, stats.DocumentCount, stats.UploadCount),
	)
	http.ServeContent(w, r, filename, time.Now(), file)
}

func (a *app) handleDocumentExport(w http.ResponseWriter, r *http.Request, user User) {
	id, err := parseID(r.URL.Path, "/api/export/documents/")
	if err != nil {
		notFound(w)
		return
	}
	format, err := parseExportFormat(r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	doc, err := a.getDocument(id)
	if err != nil {
		notFound(w)
		return
	}
	content, err := a.readDocumentContent(doc)
	if err != nil {
		serverError(w, err)
		return
	}
	data, filename, contentType, err := renderDocumentExport(doc, content, format)
	if err != nil {
		serverError(w, err)
		return
	}
	a.addOperationLog(user.ID, "export.document", "document", doc.ID, fmt.Sprintf("导出文档“%s”为 %s", doc.Title, strings.ToUpper(format)))
	serveDownloadBytes(w, filename, contentType, data)
}

func (a *app) handleFolderExport(w http.ResponseWriter, r *http.Request, user User) {
	id, err := parseID(r.URL.Path, "/api/export/folders/")
	if err != nil {
		notFound(w)
		return
	}
	if id <= 0 {
		badRequest(w, "不能通过文件夹导出接口导出根节点")
		return
	}
	format, err := parseExportFormat(r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	folder, err := a.getFolder(id, false)
	if err != nil {
		notFound(w)
		return
	}
	data, count, err := a.renderFolderExport(folder, format)
	if err != nil {
		serverError(w, err)
		return
	}
	filename := exportFilename(folder.Name, "zip")
	a.addOperationLog(user.ID, "export.folder", "folder", folder.ID, fmt.Sprintf("导出文件夹“%s”为 %s ZIP，包含 %d 篇文档", folder.Name, strings.ToUpper(format), count))
	serveDownloadBytes(w, filename, "application/zip", data)
}

func (a *app) writeKnowledgeBaseExport(out io.Writer) (exportStats, error) {
	now := time.Now()
	appName, err := a.stringSetting("app_name", "Doc System")
	if err != nil {
		return exportStats{}, err
	}
	folders, err := a.listFolders()
	if err != nil {
		return exportStats{}, err
	}
	documents, err := a.listDocuments()
	if err != nil {
		return exportStats{}, err
	}

	stats := exportStats{
		AppName:          appName,
		ExportedAt:       now.Format(time.RFC3339),
		FolderCount:      len(folders),
		DocumentCount:    len(documents),
		MissingDocuments: 0,
	}

	zw := zip.NewWriter(out)
	usedEntries := map[string]bool{}
	folderPaths := exportFolderPaths(folders)
	if err := addExportFolderDirs(zw, folderPaths, usedEntries, now); err != nil {
		_ = zw.Close()
		return stats, err
	}

	for _, doc := range documents {
		folderPath := folderPaths[doc.FolderID]
		if folderPath == "" {
			folderPath = "知识库/_未归档"
		}
		name := exportDocumentFilename(doc.Title)
		entry := uniqueZipPath(path.Join(folderPath, name), doc.ID, usedEntries)
		data, err := os.ReadFile(filepath.Join(a.docsDir, doc.FilePath))
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				_ = zw.Close()
				return stats, err
			}
			stats.MissingDocuments++
			data = []byte(fmt.Sprintf("# %s\n\n> 导出提示：文档正文文件缺失：%s\n", doc.Title, doc.FilePath))
		}
		if err := addZipBytes(zw, entry, data, now); err != nil {
			_ = zw.Close()
			return stats, err
		}
	}

	uploadCount, err := addUploadsToZip(zw, a.uploadDir, usedEntries, now)
	if err != nil {
		_ = zw.Close()
		return stats, err
	}
	stats.UploadCount = uploadCount

	readme := fmt.Sprintf(`# %s 导出包

导出时间：%s

目录说明：

- `+"`知识库/`"+`：按系统左侧文件夹树导出的 Markdown 文档。
- `+"`uploads/`"+`：系统本地上传的图片和附件。
- `+"`manifest.json`"+`：本次导出的基础统计信息。

统计：

- 文件夹：%d 个
- 文档：%d 篇
- 附件：%d 个
- 正文缺失文档：%d 篇
`, appName, now.Format("2006-01-02 15:04:05"), stats.FolderCount, stats.DocumentCount, stats.UploadCount, stats.MissingDocuments)
	if err := addZipBytes(zw, "README.md", []byte(readme), now); err != nil {
		_ = zw.Close()
		return stats, err
	}

	manifest, err := json.MarshalIndent(stats, "", "  ")
	if err != nil {
		_ = zw.Close()
		return stats, err
	}
	if err := addZipBytes(zw, "manifest.json", manifest, now); err != nil {
		_ = zw.Close()
		return stats, err
	}

	return stats, zw.Close()
}

func (a *app) renderFolderExport(folder Folder, format string) ([]byte, int, error) {
	folders, err := a.listFolders()
	if err != nil {
		return nil, 0, err
	}
	documents, err := a.listDocuments()
	if err != nil {
		return nil, 0, err
	}
	folderIDs := descendantFolderIDs(folder.ID, folders)
	folderPaths := exportFolderPaths(folders)
	rootPath := folderPaths[folder.ID]
	if rootPath == "" {
		rootPath = path.Join("知识库", safeZipSegment(folder.Name))
	}
	rootParent := path.Dir(rootPath)
	if rootParent == "." {
		rootParent = ""
	}

	now := time.Now()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	usedEntries := map[string]bool{}
	if err := addFolderExportDirs(zw, folderIDs, folderPaths, rootParent, folder.Name, usedEntries, now); err != nil {
		_ = zw.Close()
		return nil, 0, err
	}
	count := 0
	for _, doc := range documents {
		if !folderIDs[doc.FolderID] {
			continue
		}
		content, err := a.readDocumentContent(doc)
		if err != nil {
			_ = zw.Close()
			return nil, 0, err
		}
		data, filename, _, err := renderDocumentExport(doc, content, format)
		if err != nil {
			_ = zw.Close()
			return nil, 0, err
		}
		docFolderPath := folderPaths[doc.FolderID]
		if docFolderPath == "" {
			docFolderPath = rootPath
		}
		relFolder := strings.TrimPrefix(docFolderPath, rootParent)
		relFolder = strings.TrimPrefix(relFolder, "/")
		if relFolder == "" || relFolder == "." {
			relFolder = safeZipSegment(folder.Name)
		}
		entry := uniqueZipPath(path.Join(relFolder, filename), doc.ID, usedEntries)
		if err := addZipBytes(zw, entry, data, now); err != nil {
			_ = zw.Close()
			return nil, 0, err
		}
		count++
	}
	readme := fmt.Sprintf(`# %s 导出包

导出时间：%s
导出格式：%s
文档数量：%d
`, folder.Name, now.Format("2006-01-02 15:04:05"), strings.ToUpper(format), count)
	if err := addZipBytes(zw, "README.md", []byte(readme), now); err != nil {
		_ = zw.Close()
		return nil, 0, err
	}
	if err := zw.Close(); err != nil {
		return nil, 0, err
	}
	return buf.Bytes(), count, nil
}

func addExportFolderDirs(zw *zip.Writer, folderPaths map[int64]string, usedEntries map[string]bool, modTime time.Time) error {
	paths := make([]string, 0, len(folderPaths))
	for _, folderPath := range folderPaths {
		if folderPath == "" {
			continue
		}
		paths = append(paths, folderPath)
	}
	sort.Strings(paths)
	for _, folderPath := range paths {
		if err := addZipDirWithParents(zw, folderPath, usedEntries, modTime); err != nil {
			return err
		}
	}
	return nil
}

func addFolderExportDirs(
	zw *zip.Writer,
	folderIDs map[int64]bool,
	folderPaths map[int64]string,
	rootParent string,
	rootName string,
	usedEntries map[string]bool,
	modTime time.Time,
) error {
	paths := make([]string, 0, len(folderIDs))
	for folderID := range folderIDs {
		folderPath := folderPaths[folderID]
		if folderPath == "" {
			continue
		}
		relFolder := strings.TrimPrefix(folderPath, rootParent)
		relFolder = strings.TrimPrefix(relFolder, "/")
		if relFolder == "" || relFolder == "." {
			relFolder = safeZipSegment(rootName)
		}
		paths = append(paths, relFolder)
	}
	sort.Strings(paths)
	for _, folderPath := range paths {
		if err := addZipDirWithParents(zw, folderPath, usedEntries, modTime); err != nil {
			return err
		}
	}
	return nil
}

func (a *app) readDocumentContent(doc Document) (string, error) {
	data, err := os.ReadFile(filepath.Join(a.docsDir, doc.FilePath))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Sprintf("# %s\n\n> 导出提示：文档正文文件缺失：%s\n", doc.Title, doc.FilePath), nil
		}
		return "", err
	}
	return string(data), nil
}

func exportFolderPaths(folders []Folder) map[int64]string {
	children := map[int64][]Folder{}
	for _, folder := range folders {
		children[folder.ParentID] = append(children[folder.ParentID], folder)
	}
	for parentID := range children {
		sort.SliceStable(children[parentID], func(i, j int) bool {
			if children[parentID][i].SortOrder == children[parentID][j].SortOrder {
				return children[parentID][i].ID < children[parentID][j].ID
			}
			return children[parentID][i].SortOrder < children[parentID][j].SortOrder
		})
	}

	paths := map[int64]string{0: "知识库"}
	var walk func(parentID int64, parentPath string)
	walk = func(parentID int64, parentPath string) {
		usedSegments := map[string]bool{}
		for _, folder := range children[parentID] {
			base := safeZipSegment(folder.Name)
			if base == "" {
				base = "folder"
			}
			segment := uniqueZipSegment(base, folder.ID, usedSegments)
			folderPath := path.Join(parentPath, segment)
			paths[folder.ID] = folderPath
			walk(folder.ID, folderPath)
		}
	}
	walk(0, "知识库")

	for _, folder := range folders {
		if paths[folder.ID] != "" {
			continue
		}
		paths[folder.ID] = path.Join("知识库", "_未归档", uniqueZipSegment(safeZipSegment(folder.Name), folder.ID, map[string]bool{}))
	}
	return paths
}

func addUploadsToZip(zw *zip.Writer, uploadDir string, usedEntries map[string]bool, now time.Time) (int, error) {
	count := 0
	if _, err := os.Stat(uploadDir); errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	err := filepath.WalkDir(uploadDir, func(filePath string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(uploadDir, filePath)
		if err != nil {
			return err
		}
		entry := path.Join("uploads", filepath.ToSlash(rel))
		entry = uniqueZipPath(entry, int64(count+1), usedEntries)
		if err := addZipFile(zw, entry, filePath, now); err != nil {
			return err
		}
		count++
		return nil
	})
	return count, err
}

func addZipBytes(zw *zip.Writer, name string, data []byte, modTime time.Time) error {
	header := &zip.FileHeader{
		Name:   name,
		Method: zip.Deflate,
	}
	header.SetModTime(modTime)
	writer, err := zw.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = writer.Write(data)
	return err
}

func addZipDirWithParents(zw *zip.Writer, name string, usedEntries map[string]bool, modTime time.Time) error {
	name = strings.Trim(path.Clean(name), "/")
	if name == "" || name == "." {
		return nil
	}
	current := ""
	for _, segment := range strings.Split(name, "/") {
		if segment == "" || segment == "." {
			continue
		}
		current = path.Join(current, segment)
		entry := current + "/"
		if usedEntries[entry] {
			continue
		}
		header := &zip.FileHeader{
			Name:   entry,
			Method: zip.Store,
		}
		header.SetMode(os.ModeDir | 0755)
		header.SetModTime(modTime)
		if _, err := zw.CreateHeader(header); err != nil {
			return err
		}
		usedEntries[entry] = true
	}
	return nil
}

func addZipFile(zw *zip.Writer, name string, filePath string, modTime time.Time) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()
	header := &zip.FileHeader{
		Name:   name,
		Method: zip.Deflate,
	}
	if info, err := file.Stat(); err == nil {
		header.SetModTime(info.ModTime())
	} else {
		header.SetModTime(modTime)
	}
	writer, err := zw.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = io.Copy(writer, file)
	return err
}

func exportDocumentFilename(title string) string {
	name := safeZipSegment(title)
	if name == "" {
		name = "untitled"
	}
	ext := strings.ToLower(path.Ext(name))
	if ext != ".md" && ext != ".markdown" {
		name += ".md"
	}
	return name
}

func parseExportFormat(r *http.Request) (string, error) {
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "" {
		format = "md"
	}
	switch format {
	case "md", "markdown":
		return "md", nil
	case "html", "pdf":
		return format, nil
	default:
		return "", fmt.Errorf("不支持的导出格式：%s", format)
	}
}

func renderDocumentExport(doc Document, content string, format string) ([]byte, string, string, error) {
	switch format {
	case "md":
		return []byte(content), exportFilename(doc.Title, "md"), "text/markdown; charset=utf-8", nil
	case "html":
		data := renderMarkdownHTML(doc.Title, content)
		return []byte(data), exportFilename(doc.Title, "html"), "text/html; charset=utf-8", nil
	case "pdf":
		data, err := renderMarkdownPDF(doc.Title, content)
		return data, exportFilename(doc.Title, "pdf"), "application/pdf", err
	default:
		return nil, "", "", fmt.Errorf("不支持的导出格式：%s", format)
	}
}

func exportFilename(title string, ext string) string {
	name := safeZipSegment(title)
	if name == "" {
		name = "untitled"
	}
	return strings.TrimSuffix(name, path.Ext(name)) + "." + ext
}

func serveDownloadBytes(w http.ResponseWriter, filename string, contentType string, data []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", contentDisposition(filename))
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
}

func contentDisposition(filename string) string {
	ascii := safeASCIIName(filename)
	return fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, ascii, urlPathEscape(filename))
}

func safeASCIIName(filename string) string {
	var b strings.Builder
	for _, r := range filename {
		if r >= 32 && r <= 126 && r != '"' && r != '\\' && r != ';' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	name := strings.Trim(b.String(), "._ ")
	if name == "" {
		return "download"
	}
	return name
}

func urlPathEscape(value string) string {
	replacer := strings.NewReplacer("+", "%20")
	return replacer.Replace(url.QueryEscape(value))
}

func renderMarkdownHTML(title string, markdown string) string {
	body := markdownToHTML(markdown)
	return "<!doctype html>\n<html lang=\"zh-CN\">\n<head>\n<meta charset=\"utf-8\">\n" +
		"<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n" +
		"<title>" + html.EscapeString(title) + "</title>\n" +
		"<style>body{max-width:880px;margin:40px auto;padding:0 24px;font-family:-apple-system,BlinkMacSystemFont,\"Segoe UI\",\"PingFang SC\",Arial,sans-serif;line-height:1.75;color:#1f2937}pre{padding:14px;background:#f6f8fa;overflow:auto;border-radius:6px}code{background:#f6f8fa;padding:2px 4px;border-radius:4px}pre code{padding:0;background:transparent}blockquote{margin:16px 0;padding:1px 16px;color:#667085;border-left:4px solid #d0d7de}table{border-collapse:collapse;width:100%;margin:16px 0}td,th{border:1px solid #d0d7de;padding:6px 8px}img{max-width:100%}a{color:#2563eb}</style>\n" +
		"</head>\n<body>\n" + body + "\n</body>\n</html>\n"
}

func markdownToHTML(markdown string) string {
	lines := strings.Split(strings.ReplaceAll(markdown, "\r\n", "\n"), "\n")
	var b strings.Builder
	inCode := false
	inList := false
	inQuote := false
	var paragraph []string

	flushParagraph := func() {
		if len(paragraph) == 0 {
			return
		}
		b.WriteString("<p>")
		b.WriteString(inlineMarkdownHTML(strings.Join(paragraph, " ")))
		b.WriteString("</p>\n")
		paragraph = nil
	}
	closeList := func() {
		if inList {
			b.WriteString("</ul>\n")
			inList = false
		}
	}
	closeQuote := func() {
		if inQuote {
			b.WriteString("</blockquote>\n")
			inQuote = false
		}
	}

	for _, raw := range lines {
		line := strings.TrimRight(raw, " \t")
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			flushParagraph()
			closeList()
			closeQuote()
			if inCode {
				b.WriteString("</code></pre>\n")
			} else {
				b.WriteString("<pre><code>")
			}
			inCode = !inCode
			continue
		}
		if inCode {
			b.WriteString(html.EscapeString(line))
			b.WriteByte('\n')
			continue
		}
		if trimmed == "" {
			flushParagraph()
			closeList()
			closeQuote()
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			level := 0
			for level < len(trimmed) && trimmed[level] == '#' {
				level++
			}
			if level >= 1 && level <= 6 && len(trimmed) > level && trimmed[level] == ' ' {
				flushParagraph()
				closeList()
				closeQuote()
				text := strings.TrimSpace(trimmed[level:])
				b.WriteString(fmt.Sprintf("<h%d>%s</h%d>\n", level, inlineMarkdownHTML(text), level))
				continue
			}
		}
		if strings.HasPrefix(trimmed, ">") {
			flushParagraph()
			closeList()
			if !inQuote {
				b.WriteString("<blockquote>\n")
				inQuote = true
			}
			b.WriteString("<p>")
			b.WriteString(inlineMarkdownHTML(strings.TrimSpace(strings.TrimPrefix(trimmed, ">"))))
			b.WriteString("</p>\n")
			continue
		}
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			flushParagraph()
			closeQuote()
			if !inList {
				b.WriteString("<ul>\n")
				inList = true
			}
			b.WriteString("<li>")
			b.WriteString(inlineMarkdownHTML(strings.TrimSpace(trimmed[2:])))
			b.WriteString("</li>\n")
			continue
		}
		closeList()
		closeQuote()
		paragraph = append(paragraph, trimmed)
	}
	flushParagraph()
	closeList()
	closeQuote()
	if inCode {
		b.WriteString("</code></pre>\n")
	}
	return b.String()
}

func inlineMarkdownHTML(text string) string {
	escaped := html.EscapeString(text)
	imageRe := regexp.MustCompile(`!\[([^\]]*)\]\(([^)]+)\)`)
	escaped = imageRe.ReplaceAllString(escaped, `<img alt="$1" src="$2">`)
	linkRe := regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	escaped = linkRe.ReplaceAllString(escaped, `<a href="$2">$1</a>`)
	codeRe := regexp.MustCompile("`([^`]+)`")
	escaped = codeRe.ReplaceAllString(escaped, `<code>$1</code>`)
	return escaped
}

func renderMarkdownPDF(title string, markdown string) ([]byte, error) {
	lines := markdownPlainTextLines(title, markdown)
	pages := paginatePDFLines(lines, 36)
	return buildSimplePDF(pages), nil
}

func markdownPlainTextLines(title string, markdown string) []string {
	lines := []string{title, ""}
	inCode := false
	for _, raw := range strings.Split(strings.ReplaceAll(markdown, "\r\n", "\n"), "\n") {
		line := strings.TrimRight(raw, " \t")
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inCode = !inCode
			continue
		}
		if !inCode {
			line = strings.TrimSpace(line)
			line = strings.TrimPrefix(line, "#")
			line = strings.TrimSpace(line)
			line = strings.TrimPrefix(line, ">")
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
				line = "• " + strings.TrimSpace(line[2:])
			}
			line = regexp.MustCompile(`!\[([^\]]*)\]\(([^)]+)\)`).ReplaceAllString(line, "[图片: $1]")
			line = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`).ReplaceAllString(line, "$1 ($2)")
			line = strings.ReplaceAll(line, "`", "")
		}
		for _, part := range wrapRunes(line, 42) {
			lines = append(lines, part)
		}
	}
	return lines
}

func paginatePDFLines(lines []string, pageSize int) [][]string {
	if len(lines) == 0 {
		return [][]string{{""}}
	}
	var pages [][]string
	for len(lines) > 0 {
		n := pageSize
		if len(lines) < n {
			n = len(lines)
		}
		pages = append(pages, lines[:n])
		lines = lines[n:]
	}
	return pages
}

func wrapRunes(text string, max int) []string {
	runes := []rune(text)
	if len(runes) == 0 {
		return []string{""}
	}
	var lines []string
	for len(runes) > max {
		cut := max
		for i := max - 1; i > max/2; i-- {
			if unicode.IsSpace(runes[i]) {
				cut = i
				break
			}
		}
		lines = append(lines, strings.TrimSpace(string(runes[:cut])))
		runes = runes[cut:]
		for len(runes) > 0 && unicode.IsSpace(runes[0]) {
			runes = runes[1:]
		}
	}
	lines = append(lines, strings.TrimSpace(string(runes)))
	return lines
}

func buildSimplePDF(pages [][]string) []byte {
	var buf bytes.Buffer
	offsets := []int{0}
	writeObj := func(id int, body string) {
		for len(offsets) <= id {
			offsets = append(offsets, 0)
		}
		offsets[id] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", id, body)
	}
	buf.WriteString("%PDF-1.4\n")
	pageCount := len(pages)
	fontID := 3 + pageCount*2
	writeObj(1, "<< /Type /Catalog /Pages 2 0 R >>")
	var kids strings.Builder
	for i := 0; i < pageCount; i++ {
		fmt.Fprintf(&kids, "%d 0 R ", 3+i*2)
	}
	writeObj(2, fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", kids.String(), pageCount))
	for i, lines := range pages {
		pageID := 3 + i*2
		contentID := pageID + 1
		content := pdfPageContent(lines)
		writeObj(pageID, fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 %d 0 R >> >> /Contents %d 0 R >>", fontID, contentID))
		writeObj(contentID, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content))
	}
	writeObj(fontID, "<< /Type /Font /Subtype /Type0 /BaseFont /STSong-Light /Encoding /UniGB-UCS2-H /DescendantFonts [<< /Type /Font /Subtype /CIDFontType0 /BaseFont /STSong-Light /CIDSystemInfo << /Registry (Adobe) /Ordering (GB1) /Supplement 2 >> >>] >>")
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for i := 1; i < len(offsets); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xref)
	return buf.Bytes()
}

func pdfPageContent(lines []string) string {
	var b strings.Builder
	b.WriteString("BT\n/F1 12 Tf\n50 790 Td\n18 TL\n")
	for _, line := range lines {
		b.WriteString("<")
		b.WriteString(utf16BEHex(line))
		b.WriteString("> Tj\nT*\n")
	}
	b.WriteString("ET")
	return b.String()
}

func utf16BEHex(value string) string {
	var b strings.Builder
	for _, r := range value {
		if r <= 0xFFFF {
			fmt.Fprintf(&b, "%04X", r)
			continue
		}
		r -= 0x10000
		hi := 0xD800 + (r >> 10)
		lo := 0xDC00 + (r & 0x3FF)
		fmt.Fprintf(&b, "%04X%04X", hi, lo)
	}
	return b.String()
}

func descendantFolderIDs(rootID int64, folders []Folder) map[int64]bool {
	children := map[int64][]int64{}
	for _, folder := range folders {
		children[folder.ParentID] = append(children[folder.ParentID], folder.ID)
	}
	ids := map[int64]bool{rootID: true}
	var walk func(int64)
	walk = func(id int64) {
		for _, childID := range children[id] {
			if ids[childID] {
				continue
			}
			ids[childID] = true
			walk(childID)
		}
	}
	walk(rootID)
	return ids
}

func safeZipSegment(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		replace := r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|' || unicode.IsControl(r)
		if replace {
			if !lastDash {
				b.WriteRune('-')
				lastDash = true
			}
			continue
		}
		b.WriteRune(r)
		lastDash = false
	}
	cleaned := strings.Trim(strings.TrimSpace(b.String()), ". ")
	if cleaned == "" || cleaned == "." || cleaned == ".." {
		return ""
	}
	return cleaned
}

func uniqueZipSegment(base string, id int64, used map[string]bool) string {
	if base == "" {
		base = "item"
	}
	if !used[base] {
		used[base] = true
		return base
	}
	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if stem == "" {
		stem = "item"
	}
	for i := 0; ; i++ {
		suffix := strconv.FormatInt(id, 10)
		if i > 0 {
			suffix += "-" + strconv.Itoa(i)
		}
		candidate := stem + "-" + suffix + ext
		if !used[candidate] {
			used[candidate] = true
			return candidate
		}
	}
}

func uniqueZipPath(entry string, id int64, used map[string]bool) string {
	entry = path.Clean(filepath.ToSlash(entry))
	if entry == "." || strings.HasPrefix(entry, "../") || strings.HasPrefix(entry, "/") {
		entry = "item"
	}
	if !used[entry] {
		used[entry] = true
		return entry
	}
	dir, file := path.Split(entry)
	ext := path.Ext(file)
	stem := strings.TrimSuffix(file, ext)
	if stem == "" {
		stem = "item"
	}
	for i := 0; ; i++ {
		suffix := strconv.FormatInt(id, 10)
		if i > 0 {
			suffix += "-" + strconv.Itoa(i)
		}
		candidate := path.Join(dir, stem+"-"+suffix+ext)
		if !used[candidate] {
			used[candidate] = true
			return candidate
		}
	}
}
