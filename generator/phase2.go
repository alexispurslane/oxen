package generator

import (
	"bytes"
	"fmt"
	"html/template"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/niklasfasching/go-org/org"
)

// SetupTemplates loads and parses HTML templates from the templates directory
// or from the embedded filesystem. Returns the parsed templates and the base template
// modification time for cache validation.
func SetupTemplates(absPath string) (*template.Template, *template.Template, *template.Template, time.Time, error) {
	funcMap := template.FuncMap{
		"pathNoExt": func(path string) string {
			return strings.TrimSuffix(path, ".org")
		},
	}

	templatesDir := filepath.Join(absPath, "templates")
	useFS := true
	if _, statErr := os.Stat(templatesDir); os.IsNotExist(statErr) {
		useFS = false
		slog.Debug("Using embedded templates", "reason", "templates directory not found")
	} else {
		slog.Debug("Using custom templates from directory", "path", templatesDir)
	}

	var pageTmpl, tagTmpl, indexTmpl *template.Template

	if useFS {
		baseTmplPath := filepath.Join(templatesDir, "base-template.html")
		baseTmpl, err := template.New("base-template.html").Funcs(funcMap).ParseFiles(baseTmplPath)
		if err != nil {
			return nil, nil, nil, time.Time{}, fmt.Errorf("failed to parse base template: %w", err)
		}

		pageTmpl, err = template.Must(baseTmpl.Clone()).ParseFiles(
			filepath.Join(templatesDir, "page-template.html"),
		)
		if err != nil {
			return nil, nil, nil, time.Time{}, fmt.Errorf("failed to parse page template: %w", err)
		}

		tagTmpl, err = template.Must(baseTmpl.Clone()).ParseFiles(
			filepath.Join(templatesDir, "tag-page-template.html"),
		)
		if err != nil {
			return nil, nil, nil, time.Time{}, fmt.Errorf("failed to parse tag template: %w", err)
		}

		indexTmpl, err = template.Must(baseTmpl.Clone()).ParseFiles(
			filepath.Join(templatesDir, "index-page-template.html"),
		)
		if err != nil {
			return nil, nil, nil, time.Time{}, fmt.Errorf("failed to parse index template: %w", err)
		}

		info, err := os.Stat(baseTmplPath)
		if err != nil {
			return pageTmpl, tagTmpl, indexTmpl, time.Time{}, nil
		}
		return pageTmpl, tagTmpl, indexTmpl, info.ModTime(), nil
	} else {
		var err error
		pageTmpl, err = template.New("page-template.html").Funcs(funcMap).ParseFS(templates,
			"templates/base-template.html",
			"templates/page-template.html",
		)
		if err != nil {
			return nil, nil, nil, time.Time{}, fmt.Errorf("failed to parse page template: %w", err)
		}

		tagTmpl, err = template.New("tag-page-template.html").Funcs(funcMap).ParseFS(templates,
			"templates/base-template.html",
			"templates/tag-page-template.html",
		)
		if err != nil {
			return nil, nil, nil, time.Time{}, fmt.Errorf("failed to parse tag template: %w", err)
		}

		indexTmpl, err = template.New("index-page-template.html").Funcs(funcMap).ParseFS(templates,
			"templates/base-template.html",
			"templates/index-page-template.html",
		)
		if err != nil {
			return nil, nil, nil, time.Time{}, fmt.Errorf("failed to parse index template: %w", err)
		}
		return pageTmpl, tagTmpl, indexTmpl, time.Now(), nil
	}
}

// GenerateHtmlPages converts each parsed .org file to HTML and writes the result
// to ctx.DestDir. Uses UUID map to replace internal links with proper file paths.
// Returns a GenerationResult with counts of generated, skipped, and errored files.
func GenerateHtmlPages(procFiles *ProcessedFiles, ctx BuildContext, tmpl *template.Template) GenerationResult {
	slog.Debug("Starting Phase 2: generating HTML pages", "file_count", len(procFiles.Files))

	uuidToPath := make(map[string]HeaderLocation)
	procFiles.UuidMap.Range(func(key, value any) bool {
		if k, ok := key.(string); ok {
			if v, ok := value.(HeaderLocation); ok {
				uuidToPath[k] = v
			} else {
				slog.Warn("Warning: unexpected type in UuidMap value", "value", value)
			}
		} else {
			slog.Warn("Warning: unexpected type in UuidMap key", "key", key)
		}
		return true
	})

	slog.Debug("Built UUID lookup map", "uuid_count", len(uuidToPath))

	var wg sync.WaitGroup
	var filesGenerated int64
	var errors int64

	for _, fi := range procFiles.Files {
		wg.Add(1)
		go func(fi FileInfo) {
			defer wg.Done()

			if err := generateHTML(fi, ctx, uuidToPath, tmpl); err != nil {
				atomic.AddInt64(&errors, 1)
			} else {
				atomic.AddInt64(&filesGenerated, 1)
			}
		}(fi)
	}

	wg.Wait()

	slog.Debug("Phase 2 complete", "files_generated", filesGenerated, "errors", errors)

	return GenerationResult{
		FilesGenerated: int(filesGenerated),
		Errors:         int(errors),
	}
}

func generateHTML(fi FileInfo, ctx BuildContext, uuidToPath map[string]HeaderLocation, tmpl *template.Template) error {
	if fi.Path == "sitemap-preamble.org" {
		slog.Debug("Skipping sitemap-preamble.org from HTML generation")
		return nil
	}

	slog.Debug("Generating HTML for file", "path", fi.Path)
	publicDir := ctx.DestDir
	htmlRelativePath := strings.TrimSuffix(fi.Path, ".org") + ".html"
	outputPath := filepath.Join(publicDir, htmlRelativePath)

	if !ctx.ForceRebuild {
		if htmlInfo, err := os.Stat(outputPath); err == nil {
			if !fi.ModTime.After(htmlInfo.ModTime()) && !ctx.TmplModTime.After(htmlInfo.ModTime()) {
				slog.Debug("Skipping file: cache valid", "path", fi.Path)
				return nil
			}
		}
	}

	htmlContent, err := convertOrgToHTMLWithLinkReplacement(fi.ParsedOrg, fi, uuidToPath)
	if err != nil {
		slog.Warn("Error converting to HTML", "path", fi.Path, "error", err)
		return err
	}

	title := strings.TrimSuffix(fi.Path, ".org")
	title = strings.ReplaceAll(title, "_", " ")

	pageData := PageData{
		FileInfo: fi,
		Content:  template.HTML(htmlContent),
		SiteName: ctx.SiteName,
	}

	var outputBuf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&outputBuf, "page-template.html", pageData); err != nil {
		slog.Warn("Error executing template", "path", fi.Path, "error", err)
		return err
	}

	outputDir := filepath.Dir(outputPath)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		slog.Warn("Error creating directory", "path", fi.Path, "error", err)
		return err
	}

	if err := os.WriteFile(outputPath, outputBuf.Bytes(), 0644); err != nil {
		slog.Warn("Error writing file", "path", fi.Path, "error", err)
		return err
	}

	slog.Debug("Wrote HTML file", "path", outputPath)
	return nil
}

type uuidReplacingWriter struct {
	*org.HTMLWriter
	uuidToPath  map[string]HeaderLocation
	currentPath string
}

func (w *uuidReplacingWriter) WriterWithExtensions() org.Writer {
	return w
}

func (w *uuidReplacingWriter) WriteRegularLink(link org.RegularLink) {
	if link.Protocol == "id" && strings.HasPrefix(link.URL, "id:") {
		uuid := strings.TrimPrefix(link.URL, "id:")
		if len(uuid) >= 36 && isValidUUID(uuid) {
			if targetPath, ok := w.uuidToPath[uuid]; ok {
				currentDir := filepath.Dir(w.currentPath)
				targetDir := filepath.Dir(targetPath.FilePath)
				relPath, err := filepath.Rel(currentDir, targetDir)
				if err != nil {
					relPath = targetDir
				}
				baseName := strings.TrimSuffix(filepath.Base(targetPath.FilePath), ".org") + ".html"
				relativeTarget := filepath.Join(relPath, baseName)
				link.Protocol = "file"
				link.URL = fmt.Sprintf("file:%s#headline-%d", relativeTarget, targetPath.HeaderIndex)
				link.AutoLink = false
			}
		}
	}
	w.HTMLWriter.WriteRegularLink(link)
}

func convertOrgToHTMLWithLinkReplacement(doc *org.Document, fi FileInfo, uuidToPath map[string]HeaderLocation) (string, error) {
	htmlWriter := org.NewHTMLWriter()
	writer := &uuidReplacingWriter{
		HTMLWriter:  htmlWriter,
		uuidToPath:  uuidToPath,
		currentPath: fi.Path,
	}
	htmlWriter.ExtendingWriter = writer
	return doc.Write(writer)
}
