package generator

import (
	"bytes"
	"html/template"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
)

// GenerateTagPages creates a tag-*.html page for each unique tag, listing all files
// bearing that tag. Writes output to ctx.DestDir. Returns a GenerationResult.
func GenerateTagPages(procFiles *ProcessedFiles, ctx BuildContext, tmpl *template.Template) (result GenerationResult) {
	publicDir := ctx.DestDir

	var wg sync.WaitGroup
	var tagPagesGenerated int64
	var errors int64

	procFiles.TagMap.Range(func(key, value any) bool {
		tag, ok := key.(string)
		if !ok {
			log.Printf("Warning: unexpected type in TagMap key: %T", key)
			return true
		}
		files, ok := value.([]FileInfo)
		if !ok {
			log.Printf("Warning: unexpected type in TagMap value for tag %s: %T", tag, value)
			return true
		}

		wg.Add(1)
		go func(tag string, files []FileInfo) {
			defer wg.Done()

			outputPath := filepath.Join(publicDir, "tag-"+tag+".html")

			if !ctx.ForceRebuild {
				if htmlInfo, err := os.Stat(outputPath); err == nil {
					if !ctx.TmplModTime.After(htmlInfo.ModTime()) {
						return
					}
				}
			}

			tagData := TagPageData{
				Title:    tag,
				Files:    files,
				SiteName: ctx.SiteName,
			}

			var outputBuf bytes.Buffer
			if err := tmpl.ExecuteTemplate(&outputBuf, "tag-page-template.html", tagData); err != nil {
				log.Printf("Failed to execute tag template for %s: %v", tag, err)
				atomic.AddInt64(&errors, 1)
				return
			}

			if err := os.WriteFile(outputPath, outputBuf.Bytes(), 0644); err != nil {
				log.Printf("Failed to write tag page %s: %v", outputPath, err)
				atomic.AddInt64(&errors, 1)
			} else {
				atomic.AddInt64(&tagPagesGenerated, 1)
			}
		}(tag, files)

		return true
	})

	wg.Wait()

	result.TagPagesGenerated = int(tagPagesGenerated)
	result.Errors = int(errors)
	return
}

// GenerateIndexPage builds the site index (index.html) displaying the five most recent
// files, all tags with file counts, and the sitemap preamble content. Returns a GenerationResult.
func GenerateIndexPage(procFiles *ProcessedFiles, ctx BuildContext, tmpl *template.Template) (result GenerationResult) {
	publicDir := ctx.DestDir
	outputPath := filepath.Join(publicDir, "index.html")

	if !ctx.ForceRebuild {
		if htmlInfo, err := os.Stat(outputPath); err == nil {
			if !ctx.TmplModTime.After(htmlInfo.ModTime()) {
				result.FilesSkipped = 1
				return
			}
		}
	}

	recentFiles := make([]FileInfo, 0, 5)
	if len(procFiles.Files) > 0 {
		sorted := make([]FileInfo, len(procFiles.Files))
		copy(sorted, procFiles.Files)
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
	procFiles.TagMap.Range(func(key, value any) bool {
		tag, ok := key.(string)
		if !ok {
			log.Printf("Warning: unexpected type in TagMap key: %T", key)
			return true
		}
		files, ok := value.([]FileInfo)
		if !ok {
			log.Printf("Warning: unexpected type in TagMap value for tag %s: %T", tag, value)
			return true
		}
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
	preamblePath := filepath.Join(ctx.Root, "sitemap-preamble.org")
	if data, err := os.ReadFile(preamblePath); err == nil {
		if htmlContent, err := convertOrgToHTML(data, "sitemap-preamble.org"); err == nil {
			preambleContent = template.HTML(htmlContent)
		}
	}

	indexData := IndexPageData{
		RecentFiles: recentFiles,
		Tags:        tags,
		Content:     preambleContent,
		SiteName:    ctx.SiteName,
	}

	var outputBuf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&outputBuf, "index-page-template.html", indexData); err != nil {
		log.Printf("Failed to execute index template: %v", err)
		result.Errors = 1
		return
	}

	if err := os.WriteFile(outputPath, outputBuf.Bytes(), 0644); err != nil {
		log.Printf("Failed to write index page: %v", err)
		result.Errors = 1
		return
	}

	result.FilesGenerated = 1
	return
}

// CopyStaticFiles copies static assets from the static directory to the output directory.
// Returns a GenerationResult with counts of copied files and errors.
func CopyStaticFiles(ctx BuildContext) (result GenerationResult) {
	staticDir := filepath.Join(ctx.Root, "static")
	publicDir := ctx.DestDir

	entries, err := os.ReadDir(staticDir)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		log.Printf("Error reading static directory: %v", err)
		result.Errors = 1
		return
	}

	if err := os.MkdirAll(publicDir, 0755); err != nil {
		log.Printf("Failed to create public directory: %v", err)
		result.Errors = 1
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
			result.Errors++
		} else {
			result.StaticFilesCopied++
		}
	}
	return
}
