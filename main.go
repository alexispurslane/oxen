package main

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"log"
	"net/http"
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
	"github.com/fsnotify/fsnotify"
	"github.com/niklasfasching/go-org/org"
	"github.com/spf13/cobra"
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
	FileInfo
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

type buildState struct {
	fileChan   chan FileInfo
	uuidMap    sync.Map
	orgFiles   []FileInfo
	orgFilesMu sync.Mutex
	tagMap     sync.Map
	destDir    string
	stats      struct {
		totalFiles     int64
		filesWithUUIDs int64
		filesGenerated int64
		filesSkipped   int64
		errors         int64
		startTime      time.Time
	}
}

func (s *buildState) reset() {
	s.fileChan = make(chan FileInfo, 1000)
	s.orgFilesMu.Lock()
	s.orgFiles = nil
	s.orgFilesMu.Unlock()
	s.uuidMap = sync.Map{}
	s.tagMap = sync.Map{}
	s.stats.totalFiles = 0
	s.stats.filesWithUUIDs = 0
	s.stats.filesGenerated = 0
	s.stats.filesSkipped = 0
	s.stats.errors = 0
	s.stats.startTime = time.Now()
}

var state buildState

var (
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

			state.fileChan <- FileInfo{
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

			state.fileChan <- FileInfo{
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
		state.uuidMap.Store("id:"+uuid, filePath)
	}

	return resultFI, nil
}

func fileProcessingWorker(root string, wg *sync.WaitGroup) {
	defer wg.Done()

	for fi := range state.fileChan {
		fileinfo, err := processFile(fi.Path, root)
		if err != nil {
			log.Printf("Error processing %s: %v", fi.Path, err)
			continue
		}

		if fileinfo == nil {
			continue
		}

		atomic.AddInt64(&state.stats.totalFiles, 1)
		if len(fileinfo.UUIDs) > 0 {
			atomic.AddInt64(&state.stats.filesWithUUIDs, 1)
		}

		state.orgFilesMu.Lock()
		state.orgFiles = append(state.orgFiles, *fileinfo)
		state.orgFilesMu.Unlock()

		for _, tag := range fileinfo.Tags {
			existing, _ := state.tagMap.LoadOrStore(tag, []FileInfo{*fileinfo})
			if existingSlice, ok := existing.([]FileInfo); ok {
				duplicate := false
				for _, f := range existingSlice {
					if f.Path == fileinfo.Path {
						duplicate = true
						break
					}
				}
				if !duplicate {
					state.tagMap.Store(tag, append(existingSlice, *fileinfo))
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
	publicDir := state.destDir
	htmlRelativePath := strings.TrimSuffix(fi.Path, ".org") + ".html"
	outputPath := filepath.Join(publicDir, htmlRelativePath)

	if !forceRebuild {
		if htmlInfo, err := os.Stat(outputPath); err == nil {
			if !fi.ModTime.After(htmlInfo.ModTime()) && !tmplModTime.After(htmlInfo.ModTime()) {
				atomic.AddInt64(&state.stats.filesSkipped, 1)
				return nil
			}
		}
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		atomic.AddInt64(&state.stats.errors, 1)
		return fmt.Errorf("failed to read file: %w", err)
	}

	replacedData := replaceUUIDLinks(data, keywords, targetPaths, fi.Path)

	htmlContent, err := convertOrgToHTML(replacedData, fi.Path)
	if err != nil {
		atomic.AddInt64(&state.stats.errors, 1)
		return fmt.Errorf("failed to convert to HTML: %w", err)
	}

	title := strings.TrimSuffix(fi.Path, ".org")
	title = strings.ReplaceAll(title, "_", " ")

	pageData := PageData{
		FileInfo: fi,
		Content:  template.HTML(htmlContent),
	}

	var outputBuf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&outputBuf, "page-template.html", pageData); err != nil {
		atomic.AddInt64(&state.stats.errors, 1)
		return fmt.Errorf("failed to execute template: %w", err)
	}

	outputDir := filepath.Dir(outputPath)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		atomic.AddInt64(&state.stats.errors, 1)
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	if err := os.WriteFile(outputPath, outputBuf.Bytes(), 0644); err != nil {
		atomic.AddInt64(&state.stats.errors, 1)
		return fmt.Errorf("failed to write HTML: %w", err)
	}

	atomic.AddInt64(&state.stats.filesGenerated, 1)
	return nil
}

func phase3(root string, workers int, tmpl *template.Template, tmplModTime time.Time, forceRebuild bool) {
	var keywords []string
	var replacements []string

	state.uuidMap.Range(func(key, value any) bool {
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
					atomic.AddInt64(&state.stats.errors, 1)
					log.Printf("Error generating %s: %v", fi.Path, err)
				}
			}
		}()
	}

	for _, fi := range state.orgFiles {
		fileQueue <- fi
	}
	close(fileQueue)
	wg.Wait()
}

func generateTagPages(root string, tmpl *template.Template, tmplModTime time.Time, forceRebuild bool) {
	publicDir := state.destDir

	state.tagMap.Range(func(key, value any) bool {
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
			atomic.AddInt64(&state.stats.errors, 1)
			log.Printf("Failed to execute tag template for %s: %v", tag, err)
			return true
		}

		if err := os.WriteFile(outputPath, outputBuf.Bytes(), 0644); err != nil {
			atomic.AddInt64(&state.stats.errors, 1)
			log.Printf("Failed to write tag page %s: %v", outputPath, err)
		} else {
			atomic.AddInt64(&state.stats.filesGenerated, 1)
		}

		return true
	})
}

func generateIndexPage(root string, tmpl *template.Template, tmplModTime time.Time, forceRebuild bool) {
	publicDir := state.destDir
	outputPath := filepath.Join(publicDir, "index.html")

	if !forceRebuild {
		if htmlInfo, err := os.Stat(outputPath); err == nil {
			if !tmplModTime.After(htmlInfo.ModTime()) {
				return
			}
		}
	}

	recentFiles := make([]FileInfo, 0, 5)
	if len(state.orgFiles) > 0 {
		sorted := make([]FileInfo, len(state.orgFiles))
		copy(sorted, state.orgFiles)
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
	state.tagMap.Range(func(key, value any) bool {
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
		atomic.AddInt64(&state.stats.errors, 1)
		log.Printf("Failed to execute index template: %v", err)
		return
	}

	if err := os.WriteFile(outputPath, outputBuf.Bytes(), 0644); err != nil {
		atomic.AddInt64(&state.stats.errors, 1)
		log.Printf("Failed to write index page: %v", err)
	} else {
		atomic.AddInt64(&state.stats.filesGenerated, 1)
	}
}

func copyStaticFiles(root string) {
	staticDir := filepath.Join(root, "static")
	publicDir := state.destDir

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
			atomic.AddInt64(&state.stats.errors, 1)
		} else {
			atomic.AddInt64(&state.stats.filesGenerated, 1)
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
	duration := time.Since(state.stats.startTime)
	fmt.Printf("\n=== Generation Complete ===\n")
	fmt.Printf("Total files scanned:    %d\n", atomic.LoadInt64(&state.stats.totalFiles))
	fmt.Printf("Files with UUIDs:       %d\n", atomic.LoadInt64(&state.stats.filesWithUUIDs))
	fmt.Printf("Files generated:        %d\n", atomic.LoadInt64(&state.stats.filesGenerated))
	fmt.Printf("Files skipped:          %d\n", atomic.LoadInt64(&state.stats.filesSkipped))
	fmt.Printf("Errors:                 %d\n", atomic.LoadInt64(&state.stats.errors))
	fmt.Printf("Duration:               %v\n", duration.Round(time.Millisecond))
}

func buildSite(root string, workers int, forceRebuild bool, destDir string) error {
	state.reset()
	absDestDir, err := filepath.Abs(destDir)
	if err != nil {
		return fmt.Errorf("error getting absolute path for destDir: %w", err)
	}
	state.destDir = absDestDir

	if !forceRebuild {
		entries, err := os.ReadDir(absDestDir)
		if err != nil {
			if os.IsNotExist(err) {
				forceRebuild = true
			}
		} else if len(entries) == 0 {
			forceRebuild = true
		}
	}

	absPath, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("error getting absolute path: %w", err)
	}

	var fileProcessingWG sync.WaitGroup

	for range workers {
		fileProcessingWG.Add(1)
		go fileProcessingWorker(absPath, &fileProcessingWG)
	}

	var dirWalkingWG sync.WaitGroup

	dirWalkingWG.Go(func() {
		walkDirectoryConcurrent(absPath, workers)
		close(state.fileChan)
	})

	fileProcessingWG.Wait()
	dirWalkingWG.Wait()

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
		return fmt.Errorf("failed to parse page template: %w", err)
	}

	tagTmpl, err := template.New("tag-page-template.html").Funcs(funcMap).ParseFS(templates,
		"templates/base-template.html",
		"templates/tag-page-template.html",
	)
	if err != nil {
		return fmt.Errorf("failed to parse tag template: %w", err)
	}

	indexTmpl, err := template.New("index-page-template.html").Funcs(funcMap).ParseFS(templates,
		"templates/base-template.html",
		"templates/index-page-template.html",
	)
	if err != nil {
		return fmt.Errorf("failed to parse index template: %w", err)
	}

	phase3(absPath, workers, pageTmpl, time.Time{}, forceRebuild)
	generateTagPages(absPath, tagTmpl, time.Time{}, forceRebuild)
	generateIndexPage(absPath, indexTmpl, time.Time{}, forceRebuild)
	copyStaticFiles(absPath)
	printStats()

	return nil
}

func runWatchMode(root string, workers int, forceRebuild bool, destDir string) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create watcher: %w", err)
	}
	defer watcher.Close()

	// Run initial build
	fmt.Println("Starting initial build...")
	if err := buildSite(root, workers, forceRebuild, destDir); err != nil {
		log.Printf("Initial build failed: %v", err)
	}

	absPath, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("error getting absolute path: %w", err)
	}

	// Add the root directory
	if err := watcher.Add(absPath); err != nil {
		return fmt.Errorf("failed to watch root directory: %w", err)
	}

	// Walk directory tree and add all subdirectories to watcher
	destDirName := filepath.Base(state.destDir)
	err = filepath.WalkDir(absPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() != destDirName {
			if err := watcher.Add(path); err != nil {
				log.Printf("Warning: failed to watch directory %s: %v", path, err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to walk directories: %w", err)
	}

	// Channel to signal rebuilds
	rebuildChan := make(chan bool, 1)
	hasPendingRebuild := false

	// Process events in a goroutine
	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}

				// Only interested in create, write, remove, rename
				if event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename) == 0 {
					continue
				}

				// Skip anything in public directory
				if strings.HasPrefix(event.Name, state.destDir) {
					continue
				}

				// If a new directory is created, watch it
				if event.Op&fsnotify.Create == fsnotify.Create {
					if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
						if err := watcher.Add(event.Name); err != nil {
							log.Printf("Warning: failed to watch new directory %s: %v", event.Name, err)
						}
					}
				}

				// Print watcher event for debugging
				log.Printf("Watcher event: %v %s", event.Op, event.Name)

				// Trigger rebuild
				if !hasPendingRebuild {
					hasPendingRebuild = true
					select {
					case rebuildChan <- true:
					default:
					}
				}

			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Printf("Watcher error: %v", err)
			}
		}
	}()

	fmt.Printf("Watching %s for changes... (Press Ctrl+C to stop)\n", absPath)

	// Main watch loop
	for {
		<-rebuildChan
		hasPendingRebuild = false

		// Debounce: wait a bit for more changes
		time.Sleep(100 * time.Millisecond)

		// Drain any additional rebuild signals
		select {
		case <-rebuildChan:
		default:
		}

		fmt.Println("\nChanges detected, rebuilding...")
		if err := buildSite(root, workers, forceRebuild, destDir); err != nil {
			log.Printf("Build failed: %v", err)
		}
		fmt.Printf("\nWatching %s for changes... (Press Ctrl+C to stop)\n", absPath)
	}
}

func runHTTPServer(dir string, port int) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("error getting absolute path: %w", err)
	}

	fmt.Printf("Serving %s on http://localhost:%d\n", absDir, port)
	return http.ListenAndServe(fmt.Sprintf(":%d", port), http.FileServer(http.Dir(absDir)))
}

var (
	dir     string
	force   bool
	watch   bool
	workers int
	port    int
	dest    string
)

func main() {
	var rootCmd = &cobra.Command{Use: "oxen"}

	var buildCmd = &cobra.Command{
		Use:   "build <dir>",
		Short: "Build the site from <dir>",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if watch {
				if err := runWatchMode(args[0], workers, force, dest); err != nil {
					log.Fatalf("Watch mode failed: %v", err)
				}
			} else {
				if err := buildSite(args[0], workers, force, dest); err != nil {
					log.Fatalf("Build failed: %v", err)
				}
			}
		},
	}

	var serveCmd = &cobra.Command{
		Use:   "serve <dir>",
		Short: "Serve the built site",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			go func() {
				if err := runHTTPServer(dest, port); err != nil {
					log.Printf("HTTP server error: %v", err)
				}
			}()

			if watch {
				if err := runWatchMode(args[0], workers, force, dest); err != nil {
					log.Fatalf("Watch mode failed: %v", err)
				}
			} else {
				if err := buildSite(args[0], workers, force, dest); err != nil {
					log.Fatalf("Build failed: %v", err)
				}
				fmt.Printf("\nServer running at http://localhost:%d\n", port)
				select {}
			}
		},
	}

	var lookupCmd = &cobra.Command{
		Use:   "lookup-id <dir> <id>",
		Short: "Find the file containing the given ID",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			absPath, err := filepath.Abs(args[0])
			if err != nil {
				log.Fatalf("Error getting absolute path: %v", err)
			}

			state.reset()
			walkDirectoryConcurrent(absPath, workers)
			close(state.fileChan)

			var fileProcessingWG sync.WaitGroup
			for range workers {
				fileProcessingWG.Add(1)
				go fileProcessingWorker(absPath, &fileProcessingWG)
			}
			fileProcessingWG.Wait()

			if path, found := state.uuidMap.Load("id:" + args[1]); found {
				fmt.Printf("ID %s found in: %s\n", args[1], path.(string))
			} else {
				fmt.Printf("ID %s not found\n", args[1])
			}
		},
	}

	buildCmd.Flags().BoolVarP(&force, "force", "f", false, "force rebuild all files")
	buildCmd.Flags().BoolVarP(&watch, "watch", "w", false, "watch for changes and rebuild")
	buildCmd.Flags().IntVarP(&workers, "workers", "j", 8, "number of concurrent workers")
	buildCmd.Flags().StringVar(&dest, "dest", "public", "output directory")

	serveCmd.Flags().BoolVarP(&force, "force", "f", false, "force rebuild all files")
	serveCmd.Flags().BoolVarP(&watch, "watch", "w", false, "watch for changes and rebuild")
	serveCmd.Flags().IntVarP(&workers, "workers", "j", 8, "number of concurrent workers")
	serveCmd.Flags().IntVarP(&port, "port", "p", 8080, "port to serve on")
	serveCmd.Flags().StringVar(&dest, "dest", "public", "output directory")

	lookupCmd.Flags().IntVarP(&workers, "workers", "j", 8, "number of concurrent workers")

	rootCmd.AddCommand(buildCmd, serveCmd, lookupCmd)
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
