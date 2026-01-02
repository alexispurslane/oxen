package generator

import (
	"bytes"
	"fmt"
	"html/template"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/anknown/ahocorasick"
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
	var keywords []string
	var replacements []string

	procFiles.UuidMap.Range(func(key, value any) bool {
		if k, ok := key.(string); ok {
			if v, ok := value.(string); ok {
				keywords = append(keywords, k)
				replacements = append(replacements, v)
			} else {
				log.Printf("Warning: unexpected type in UuidMap value: %T", value)
			}
		} else {
			log.Printf("Warning: unexpected type in UuidMap key: %T", key)
		}
		return true
	})

	var wg sync.WaitGroup
	fileQueue := make(chan FileInfo, ctx.Workers*2)
	var filesGenerated int64
	var errors int64

	for range ctx.Workers {
		wg.Go(func() {
			for fi := range fileQueue {
				if err := generateHTML(fi, ctx, keywords, replacements, tmpl); err != nil {
					atomic.AddInt64(&errors, 1)
				} else {
					atomic.AddInt64(&filesGenerated, 1)
				}
			}
		})
	}

	for _, fi := range procFiles.Files {
		fileQueue <- fi
	}
	close(fileQueue)
	wg.Wait()

	return GenerationResult{
		FilesGenerated: int(filesGenerated),
		Errors:         int(errors),
	}
}

func generateHTML(fi FileInfo, ctx BuildContext, keywords []string, targetPaths []string, tmpl *template.Template) error {
	if fi.Path == "sitemap-preamble.org" {
		return nil
	}

	absPath := filepath.Join(ctx.Root, fi.Path)
	publicDir := ctx.DestDir
	htmlRelativePath := strings.TrimSuffix(fi.Path, ".org") + ".html"
	outputPath := filepath.Join(publicDir, htmlRelativePath)

	if !ctx.ForceRebuild {
		if htmlInfo, err := os.Stat(outputPath); err == nil {
			if !fi.ModTime.After(htmlInfo.ModTime()) && !ctx.TmplModTime.After(htmlInfo.ModTime()) {
				return nil
			}
		}
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		log.Printf("Error reading %s: %v", fi.Path, err)
		return err
	}

	replacedData := replaceUUIDLinks(data, keywords, targetPaths, fi.Path)

	htmlContent, err := convertOrgToHTML(replacedData, fi.Path)
	if err != nil {
		log.Printf("Error converting %s to HTML: %v", fi.Path, err)
		return err
	}

	title := strings.TrimSuffix(fi.Path, ".org")
	title = strings.ReplaceAll(title, "_", " ")

	pageData := PageData{
		FileInfo: fi,
		Content:  template.HTML(htmlContent),
	}

	var outputBuf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&outputBuf, "page-template.html", pageData); err != nil {
		log.Printf("Error executing template for %s: %v", fi.Path, err)
		return err
	}

	outputDir := filepath.Dir(outputPath)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		log.Printf("Error creating directory for %s: %v", fi.Path, err)
		return err
	}

	if err := os.WriteFile(outputPath, outputBuf.Bytes(), 0644); err != nil {
		log.Printf("Error writing %s: %v", fi.Path, err)
		return err
	}

	return nil
}

func convertOrgToHTML(orgContent []byte, filePath string) (string, error) {
	lines := strings.Split(string(orgContent), "\n")
	if len(lines) > 0 && strings.HasPrefix(lines[0], "* ") {
		lines = lines[1:]
	}
	contentWithoutTitle := strings.Join(lines, "\n")

	conf := org.New()
	conf.DefaultSettings = map[string]string{
		"OPTIONS": "toc:nil <:t e:t f:t pri:t todo:t tags:t title:t ealb:nil",
	}
	doc := conf.Parse(bytes.NewReader([]byte(contentWithoutTitle)), filePath)
	writer := org.NewHTMLWriter()
	return doc.Write(writer)
}

func replaceUUIDLinks(data []byte, keywords []string, targetPaths []string, currentFilePath string) []byte {
	if len(keywords) == 0 || len(targetPaths) != len(keywords) {
		return data
	}

	mach := new(goahocorasick.Machine)

	var keywordRunes [][]rune
	for _, kw := range keywords {
		keywordRunes = append(keywordRunes, []rune(kw))
	}

	err := mach.Build(keywordRunes)
	if err != nil {
		log.Printf("Failed to build Aho-Corasick machine: %v", err)
		return data
	}

	content := []rune(string(data))
	terms := mach.MultiPatternSearch(content, false)

	if len(terms) == 0 {
		return data
	}

	targetPathMap := make(map[string]string)
	for i, kw := range keywords {
		targetPathMap[kw] = targetPaths[i]
	}

	currentDir := filepath.Dir(currentFilePath)

	var result []rune
	lastPos := 0

	for _, term := range terms {
		keyword := string(term.Word)
		targetPath := targetPathMap[keyword]

		relPath, err := filepath.Rel(currentDir, targetPath)
		if err != nil {
			relPath = targetPath
		}

		replacement := fmt.Sprintf("file:%s.html", strings.TrimSuffix(relPath, ".org"))

		matchStart := term.Pos
		matchEnd := term.Pos + len(term.Word)

		result = append(result, content[lastPos:matchStart]...)
		result = append(result, []rune(replacement)...)

		lastPos = matchEnd
	}

	result = append(result, content[lastPos:]...)

	return []byte(string(result))
}
