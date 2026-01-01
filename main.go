package main

import (
	"bytes"
	"embed"
	"flag"
	"fmt"
	"html/template"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/anknown/ahocorasick"
	"github.com/niklasfasching/go-org/org"
)

//go:embed templates/*.html
var templates embed.FS

type FileInfo struct {
	Path    string
	ModTime time.Time
	Preview string
	Title   string
	Tags    []string
	UUIDs   []string
}

type PageData struct {
	Path    string
	ModTime time.Time
	Preview string
	Title   string
	Tags    []string
	UUIDs   []string
	Content template.HTML
}

type TagPageData struct {
	Title string
	Files []FileInfo
}

type TagInfo struct {
	Name  string
	Count int
}

type IndexPageData struct {
	RecentFiles []FileInfo
	Tags        []TagInfo
	Content     template.HTML
}

var (
	fileChan   = make(chan FileInfo, 1000)
	uuidMap    sync.Map
	orgFiles   []FileInfo
	orgFilesMu sync.Mutex
	tagMap     sync.Map

	stats struct {
		totalFiles     int64
		filesWithUUIDs int64
		filesGenerated int64
		filesSkipped   int64
		errors         int64
		startTime      time.Time
	}

	reDrawers         = regexp.MustCompile(`(?m)^\s*:PROPERTIES:[\s\S]*?:END:(?:\s*\n|\s*$)`)
	reOrgKeywords     = regexp.MustCompile(`(?m)^\s*#\+\S+.*$`)
	reDrawerProps     = regexp.MustCompile(`(?m)^\s*:\S+:\s*$`)
	reBlocks          = regexp.MustCompile(`(?m)^\s*#\+begin_\S+.*?(?:^\s*#\+end_\S+.*?$|\z)`)
	reIncompleteBlock = regexp.MustCompile(`^\s*#\+(?:begin|end)_\S+`)
	reLinkDesc        = regexp.MustCompile(`\[\[.*?\]\[([^\]]*)\]\]`)
	reLinkFile        = regexp.MustCompile(`\[\[file:([^\]]+)\]\]`)
	reEmphasis        = regexp.MustCompile(`[*~/=_~']([^*=~_/'\[\]]+)[*~/=_~']`)
	reNewlines        = regexp.MustCompile(`\n+`)
	reWhitespace      = regexp.MustCompile(`\s+`)
)

func walkDirectory(dir string, root string) {
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			log.Printf("Error accessing path %s: %v", path, err)
			return nil
		}

		if !d.IsDir() && strings.HasSuffix(path, ".org") {
			info, err := d.Info()
			if err != nil {
				log.Printf("Error getting info for %s: %v", path, err)
				return nil
			}

			relPath := strings.TrimPrefix(path, root+string(filepath.Separator))
			if relPath == path {
				relPath = strings.TrimPrefix(path, root)
			}

			fileChan <- FileInfo{
				Path:    relPath,
				ModTime: info.ModTime(),
			}
		}

		return nil
	})

	if err != nil {
		log.Printf("Error walking directory %s: %v", dir, err)
	}
}

func walkDirectoryConcurrent(root string, workers int) {
	var wg sync.WaitGroup
	dirChan := make(chan string, workers*2)

	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for dir := range dirChan {
				walkDirectory(dir, root)
			}
		}()
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		log.Fatalf("Error reading root directory: %v", err)
	}

	for _, entry := range entries {
		fullPath := filepath.Join(root, entry.Name())

		if entry.IsDir() {
			dirChan <- fullPath
		} else if strings.HasSuffix(fullPath, ".org") {
			info, err := entry.Info()
			if err != nil {
				log.Printf("Error getting info for %s: %v", fullPath, err)
				continue
			}

			fileChan <- FileInfo{
				Path:    entry.Name(),
				ModTime: info.ModTime(),
			}
		}
	}

	close(dirChan)
	wg.Wait()
}

func extractUUIDs(data []byte) []string {
	s := string(data)
	var uuids []string

	for i := 0; i < len(s); {
		idx := strings.Index(s[i:], ":ID:")
		if idx == -1 {
			break
		}

		idx += i + 4
		for idx < len(s) && s[idx] == ' ' {
			idx++
		}

		if idx+36 <= len(s) && isValidUUID(s[idx:idx+36]) {
			uuids = append(uuids, s[idx:idx+36])
			i = idx + 36
		} else {
			i = idx + 1
		}
	}
	return uuids
}

func isValidUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	if s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return false
	}
	for _, c := range s {
		if c != '-' && !isHexChar(byte(c)) {
			return false
		}
	}
	return true
}

func isHexChar(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}

func processFile(filePath, root string) (*FileInfo, error) {
	absPath := filepath.Join(root, filePath)
	file, err := os.Open(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	size := info.Size()
	if size == 0 {
		return nil, nil
	}

	data, err := syscall.Mmap(int(file.Fd()), 0, int(size), syscall.PROT_READ, syscall.MAP_PRIVATE)
	if err != nil {
		return nil, fmt.Errorf("failed to mmap file: %w", err)
	}
	defer syscall.Munmap(data)

	resultFI := &FileInfo{
		Path:    filePath,
		ModTime: info.ModTime(),
		Preview: extractPreview(data, 500),
		Title:   extractTitle(data),
		Tags:    extractTags(data),
		UUIDs:   extractUUIDs(data),
	}

	for _, uuid := range resultFI.UUIDs {
		uuidMap.Store("id:"+uuid, filePath)
	}

	return resultFI, nil
}

func phase2Worker(root string, wg *sync.WaitGroup) {
	defer wg.Done()

	for fi := range fileChan {
		fileinfo, err := processFile(fi.Path, root)
		if err != nil {
			log.Printf("Error processing %s: %v", fi.Path, err)
			continue
		}

		if fileinfo == nil {
			continue
		}

		atomic.AddInt64(&stats.totalFiles, 1)
		if len(fileinfo.UUIDs) > 0 {
			atomic.AddInt64(&stats.filesWithUUIDs, 1)
		}

		orgFilesMu.Lock()
		orgFiles = append(orgFiles, *fileinfo)
		orgFilesMu.Unlock()

		for _, tag := range fileinfo.Tags {
			existing, _ := tagMap.LoadOrStore(tag, []FileInfo{*fileinfo})
			if existingSlice, ok := existing.([]FileInfo); ok {
				duplicate := false
				for _, f := range existingSlice {
					if f.Path == fileinfo.Path {
						duplicate = true
						break
					}
				}
				if !duplicate {
					tagMap.Store(tag, append(existingSlice, *fileinfo))
				}
			}
		}
	}
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

func extractTitle(orgContent []byte) string {
	s := string(orgContent)
	lines := strings.SplitSeq(s, "\n")

	for line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(trimmed), "#+title:") {
			title := strings.TrimSpace(trimmed[8:])
			if title != "" {
				return title
			}
		} else if strings.HasPrefix(trimmed, "* ") && trimmed != "* " {
			titlePart := strings.TrimSpace(trimmed[2:])
			if spaceIdx := strings.Index(titlePart, " :"); spaceIdx != -1 {
				titlePart = strings.TrimSpace(titlePart[:spaceIdx])
			}
			if titlePart != "" {
				return titlePart
			}
		}
	}

	return ""
}

func extractTags(orgContent []byte) []string {
	s := string(orgContent)
	lines := strings.Split(s, "\n")
	var tags []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "* ") && trimmed != "* " {
			firstSpace := strings.Index(trimmed, " :")
			if firstSpace == -1 {
				continue
			}

			tagStr := trimmed[firstSpace+1:]
			spaceAfterTag := strings.Index(tagStr, " ")
			if spaceAfterTag != -1 {
				tagStr = tagStr[:spaceAfterTag]
			}

			tagParts := strings.SplitSeq(tagStr, ":")
			for tag := range tagParts {
				if tag != "" {
					tags = append(tags, tag)
				}
			}
			break
		}
	}

	return tags
}

func extractPreview(orgContent []byte, maxLen int) string {
	if len(orgContent) == 0 {
		return ""
	}

	s := string(orgContent)
	idx := strings.Index(s, "\n")
	if idx == -1 {
		return ""
	}

	contentAfterFirstLine := s[idx+1:]

	contentAfterFirstLine = reDrawers.ReplaceAllString(contentAfterFirstLine, "")
	contentAfterFirstLine = reOrgKeywords.ReplaceAllString(contentAfterFirstLine, "")
	contentAfterFirstLine = reDrawerProps.ReplaceAllString(contentAfterFirstLine, "")
	contentAfterFirstLine = reBlocks.ReplaceAllString(contentAfterFirstLine, "")
	contentAfterFirstLine = reIncompleteBlock.ReplaceAllString(contentAfterFirstLine, "")
	contentAfterFirstLine = reLinkDesc.ReplaceAllString(contentAfterFirstLine, "$1")

	contentAfterFirstLine = reLinkFile.ReplaceAllStringFunc(contentAfterFirstLine, func(m string) string {
		parts := reLinkFile.FindStringSubmatch(m)
		if len(parts) > 1 {
			return filepath.Base(strings.TrimSuffix(parts[1], ".org"))
		}
		return m
	})

	contentAfterFirstLine = reEmphasis.ReplaceAllString(contentAfterFirstLine, "$1")
	contentAfterFirstLine = reNewlines.ReplaceAllString(contentAfterFirstLine, " ")
	contentAfterFirstLine = reWhitespace.ReplaceAllString(contentAfterFirstLine, " ")
	contentAfterFirstLine = strings.TrimSpace(contentAfterFirstLine)

	if len(contentAfterFirstLine) > maxLen {
		cutAt := maxLen
		for cutAt > 0 && contentAfterFirstLine[cutAt-1] > 127 {
			cutAt--
		}
		return contentAfterFirstLine[:cutAt] + "..."
	}
	return contentAfterFirstLine
}

func generateHTML(fi FileInfo, root string, keywords []string, targetPaths []string, tmpl *template.Template, tmplModTime time.Time, forceRebuild bool) error {
	if fi.Path == "sitemap-preamble.org" {
		return nil
	}

	absPath := filepath.Join(root, fi.Path)
	publicDir := filepath.Join(root, "public")
	htmlRelativePath := strings.TrimSuffix(fi.Path, ".org") + ".html"
	outputPath := filepath.Join(publicDir, htmlRelativePath)

	if !forceRebuild {
		if htmlInfo, err := os.Stat(outputPath); err == nil {
			if !fi.ModTime.After(htmlInfo.ModTime()) && !tmplModTime.After(htmlInfo.ModTime()) {
				atomic.AddInt64(&stats.filesSkipped, 1)
				return nil
			}
		}
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		atomic.AddInt64(&stats.errors, 1)
		return fmt.Errorf("failed to read file: %w", err)
	}

	replacedData := replaceUUIDLinks(data, keywords, targetPaths, fi.Path)

	htmlContent, err := convertOrgToHTML(replacedData, fi.Path)
	if err != nil {
		atomic.AddInt64(&stats.errors, 1)
		return fmt.Errorf("failed to convert to HTML: %w", err)
	}

	title := strings.TrimSuffix(fi.Path, ".org")
	title = strings.ReplaceAll(title, "_", " ")

	pageData := PageData{
		Path:    fi.Path,
		ModTime: fi.ModTime,
		Preview: fi.Preview,
		Title:   fi.Title,
		Tags:    fi.Tags,
		UUIDs:   fi.UUIDs,
		Content: template.HTML(htmlContent),
	}

	var outputBuf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&outputBuf, "page-template.html", pageData); err != nil {
		atomic.AddInt64(&stats.errors, 1)
		return fmt.Errorf("failed to execute template: %w", err)
	}

	outputDir := filepath.Dir(outputPath)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		atomic.AddInt64(&stats.errors, 1)
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	if err := os.WriteFile(outputPath, outputBuf.Bytes(), 0644); err != nil {
		atomic.AddInt64(&stats.errors, 1)
		return fmt.Errorf("failed to write HTML: %w", err)
	}

	atomic.AddInt64(&stats.filesGenerated, 1)
	return nil
}

func phase3(root string, workers int, tmpl *template.Template, tmplModTime time.Time, forceRebuild bool) {
	var keywords []string
	var replacements []string

	uuidMap.Range(func(key, value any) bool {
		keywords = append(keywords, key.(string))
		replacements = append(replacements, value.(string))
		return true
	})

	var wg sync.WaitGroup
	fileQueue := make(chan FileInfo, workers*2)

	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for fi := range fileQueue {
				if err := generateHTML(fi, root, keywords, replacements, tmpl, tmplModTime, forceRebuild); err != nil {
					atomic.AddInt64(&stats.errors, 1)
					log.Printf("Error generating %s: %v", fi.Path, err)
				}
			}
		}()
	}

	for _, fi := range orgFiles {
		fileQueue <- fi
	}
	close(fileQueue)
	wg.Wait()
}

func generateTagPages(root string, tmpl *template.Template, tmplModTime time.Time, forceRebuild bool) {
	publicDir := filepath.Join(root, "public")

	tagMap.Range(func(key, value any) bool {
		tag := key.(string)
		files := value.([]FileInfo)

		outputPath := filepath.Join(publicDir, "tag-"+tag+".html")

		if !forceRebuild {
			if htmlInfo, err := os.Stat(outputPath); err == nil {
				if !tmplModTime.After(htmlInfo.ModTime()) {
					return true
				}
			}
		}

		tagData := TagPageData{
			Title: tag,
			Files: files,
		}

		var outputBuf bytes.Buffer
		if err := tmpl.ExecuteTemplate(&outputBuf, "tag-page-template.html", tagData); err != nil {
			atomic.AddInt64(&stats.errors, 1)
			log.Printf("Failed to execute tag template for %s: %v", tag, err)
			return true
		}

		if err := os.WriteFile(outputPath, outputBuf.Bytes(), 0644); err != nil {
			atomic.AddInt64(&stats.errors, 1)
			log.Printf("Failed to write tag page %s: %v", outputPath, err)
		} else {
			atomic.AddInt64(&stats.filesGenerated, 1)
		}

		return true
	})
}

func generateIndexPage(root string, tmpl *template.Template, tmplModTime time.Time, forceRebuild bool) {
	publicDir := filepath.Join(root, "public")
	outputPath := filepath.Join(publicDir, "index.html")

	if !forceRebuild {
		if htmlInfo, err := os.Stat(outputPath); err == nil {
			if !tmplModTime.After(htmlInfo.ModTime()) {
				return
			}
		}
	}

	recentFiles := make([]FileInfo, 0, 5)
	if len(orgFiles) > 0 {
		sorted := make([]FileInfo, len(orgFiles))
		copy(sorted, orgFiles)
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].ModTime.After(sorted[j].ModTime)
		})
		if len(sorted) > 5 {
			recentFiles = sorted[:5]
		} else {
			recentFiles = sorted
		}
	}

	var tags []TagInfo
	tagMap.Range(func(key, value any) bool {
		tag := key.(string)
		files := value.([]FileInfo)
		tags = append(tags, TagInfo{
			Name:  tag,
			Count: len(files),
		})
		return true
	})
	sort.Slice(tags, func(i, j int) bool {
		return tags[i].Name < tags[j].Name
	})

	var preambleContent template.HTML
	preamblePath := filepath.Join(root, "sitemap-preamble.org")
	if data, err := os.ReadFile(preamblePath); err == nil {
		if htmlContent, err := convertOrgToHTML(data, "sitemap-preamble.org"); err == nil {
			preambleContent = template.HTML(htmlContent)
		}
	}

	indexData := IndexPageData{
		RecentFiles: recentFiles,
		Tags:        tags,
		Content:     preambleContent,
	}

	var outputBuf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&outputBuf, "index-page-template.html", indexData); err != nil {
		atomic.AddInt64(&stats.errors, 1)
		log.Printf("Failed to execute index template: %v", err)
		return
	}

	if err := os.WriteFile(outputPath, outputBuf.Bytes(), 0644); err != nil {
		atomic.AddInt64(&stats.errors, 1)
		log.Printf("Failed to write index page: %v", err)
	} else {
		atomic.AddInt64(&stats.filesGenerated, 1)
	}
}

func copyStaticFiles(root string) {
	staticDir := filepath.Join(root, "static")
	publicDir := filepath.Join(root, "public")

	entries, err := os.ReadDir(staticDir)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		log.Printf("Error reading static directory: %v", err)
		return
	}

	if err := os.MkdirAll(publicDir, 0755); err != nil {
		log.Printf("Failed to create public directory: %v", err)
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		srcPath := filepath.Join(staticDir, entry.Name())
		dstPath := filepath.Join(publicDir, entry.Name())

		if err := copyFile(srcPath, dstPath); err != nil {
			log.Printf("Failed to copy %s: %v", entry.Name(), err)
			atomic.AddInt64(&stats.errors, 1)
		} else {
			atomic.AddInt64(&stats.filesGenerated, 1)
		}
	}
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}

func printStats() {
	duration := time.Since(stats.startTime)
	fmt.Printf("\n=== Generation Complete ===\n")
	fmt.Printf("Total files scanned:    %d\n", atomic.LoadInt64(&stats.totalFiles))
	fmt.Printf("Files with UUIDs:       %d\n", atomic.LoadInt64(&stats.filesWithUUIDs))
	fmt.Printf("Files generated:        %d\n", atomic.LoadInt64(&stats.filesGenerated))
	fmt.Printf("Files skipped:          %d\n", atomic.LoadInt64(&stats.filesSkipped))
	fmt.Printf("Errors:                 %d\n", atomic.LoadInt64(&stats.errors))
	fmt.Printf("Duration:               %v\n", duration.Round(time.Millisecond))
}

func main() {
	dir := flag.String("d", ".", "relative path to directory to scan")
	workers := flag.Int("w", 8, "number of concurrent workers")
	force := flag.Bool("force", false, "force rebuild all files")
	findID := flag.String("find-id", "", "find the file containing the specified ID")
	flag.Parse()

	if *findID != "" {
		absPath, err := filepath.Abs(*dir)
		if err != nil {
			log.Fatalf("Error getting absolute path: %v", err)
		}

		walkDirectoryConcurrent(absPath, *workers)
		close(fileChan)

		var phase2WG sync.WaitGroup
		for i := 0; i < *workers; i++ {
			phase2WG.Add(1)
			go phase2Worker(absPath, &phase2WG)
		}
		phase2WG.Wait()

		if path, found := uuidMap.Load("id:" + *findID); found {
			fmt.Printf("ID %s found in: %s\n", *findID, path.(string))
		} else {
			fmt.Printf("ID %s not found\n", *findID)
		}
		return
	}

	stats.startTime = time.Now()

	absPath, err := filepath.Abs(*dir)
	if err != nil {
		log.Fatalf("Error getting absolute path: %v", err)
	}

	var phase2WG sync.WaitGroup

	for i := 0; i < *workers; i++ {
		phase2WG.Add(1)
		go phase2Worker(absPath, &phase2WG)
	}

	var phase1WG sync.WaitGroup

	phase1WG.Go(func() {
		walkDirectoryConcurrent(absPath, *workers)
		close(fileChan)
	})

	phase2WG.Wait()
	phase1WG.Wait()

	funcMap := template.FuncMap{
		"pathNoExt": func(path string) string {
			return strings.TrimSuffix(path, ".org")
		},
	}

	pageTmpl, err := template.New("page-template.html").Funcs(funcMap).ParseFS(templates,
		"templates/base-template.html",
		"templates/page-template.html",
	)
	if err != nil {
		log.Fatalf("Failed to parse page template: %v", err)
	}

	tagTmpl, err := template.New("tag-page-template.html").Funcs(funcMap).ParseFS(templates,
		"templates/base-template.html",
		"templates/tag-page-template.html",
	)
	if err != nil {
		log.Fatalf("Failed to parse tag template: %v", err)
	}

	indexTmpl, err := template.New("index-page-template.html").Funcs(funcMap).ParseFS(templates,
		"templates/base-template.html",
		"templates/index-page-template.html",
	)
	if err != nil {
		log.Fatalf("Failed to parse index template: %v", err)
	}

	phase3(absPath, *workers, pageTmpl, time.Time{}, *force)
	generateTagPages(absPath, tagTmpl, time.Time{}, *force)
	generateIndexPage(absPath, indexTmpl, time.Time{}, *force)
	copyStaticFiles(absPath)
	printStats()
}
