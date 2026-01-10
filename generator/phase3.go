package generator

import (
	"bytes"
	"html/template"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/niklasfasching/go-org/org"
)

// GenerateTagPages creates a tag-*.html page for each unique tag, listing all files
// bearing that tag. Writes output to ctx.DestDir. Returns a GenerationResult.
func GenerateTagPages(procFiles *ProcessedFiles, ctx BuildContext, tmpl *template.Template) (result GenerationResult) {
	slog.Debug("Starting Phase 3a: generating tag pages")

	publicDir := ctx.DestDir

	var wg sync.WaitGroup
	var tagPagesGenerated int64
	var errors int64

	procFiles.TagMap.Range(func(key, value any) bool {
		tag, ok := key.(string)
		if !ok {
			slog.Warn("Warning: unexpected type in TagMap key", "key", key)
			return true
		}
		files, ok := value.([]FileInfo)
		if !ok {
			slog.Warn("Warning: unexpected type in TagMap value", "tag", tag, "value", value)
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
				Title:        tag,
				Files:        files,
				SiteName:     ctx.SiteName,
				BaseURL:      ctx.BaseURL,
				DefaultImage: ctx.DefaultImage,
				Author:       ctx.Author,
				LicenseName:  ctx.LicenseName,
				LicenseURL:   ctx.LicenseURL,
			}

			var outputBuf bytes.Buffer
			if err := tmpl.ExecuteTemplate(&outputBuf, "tag-page-template.html", tagData); err != nil {
				slog.Warn("Failed to execute tag template", "tag", tag, "error", err)
				atomic.AddInt64(&errors, 1)
				return
			}

			if err := os.WriteFile(outputPath, outputBuf.Bytes(), 0644); err != nil {
				slog.Warn("Failed to write tag page", "path", outputPath, "error", err)
				atomic.AddInt64(&errors, 1)
			} else {
				slog.Debug("Generated tag page", "tag", tag, "path", outputPath, "file_count", len(files))
				atomic.AddInt64(&tagPagesGenerated, 1)
			}
		}(tag, files)

		return true
	})

	wg.Wait()

	slog.Debug("Phase 3a complete", "tag_pages_generated", tagPagesGenerated, "errors", errors)

	result.TagPagesGenerated = int(tagPagesGenerated)
	result.Errors = int(errors)
	return
}

// GenerateIndexPage builds the site index (index.html) displaying the five most recent
// files, all tags with file counts, and the sitemap preamble content. Returns a GenerationResult.
func GenerateIndexPage(procFiles *ProcessedFiles, ctx BuildContext, tmpl *template.Template) (result GenerationResult) {
	slog.Debug("Starting Phase 3b: generating index page")

	publicDir := ctx.DestDir
	outputPath := filepath.Join(publicDir, "index.html")

	if !ctx.ForceRebuild {
		if htmlInfo, err := os.Stat(outputPath); err == nil {
			if !ctx.TmplModTime.After(htmlInfo.ModTime()) {
				slog.Debug("Skipping index page: cache valid")
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
			slog.Warn("Warning: unexpected type in TagMap key", "key", key)
			return true
		}
		files, ok := value.([]FileInfo)
		if !ok {
			slog.Warn("Warning: unexpected type in TagMap value", "tag", tag, "value", value)
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
		conf := org.New()
		conf.DefaultSettings = map[string]string{
			"OPTIONS": "toc:nil <:t e:t f:t pri:t todo:t tags:t title:t ealb:nil",
		}
		doc := conf.Parse(bytes.NewReader(data), "sitemap-preamble.org")
		writer := org.NewHTMLWriter()
		if htmlContent, err := doc.Write(writer); err == nil {
			preambleContent = template.HTML(htmlContent)
		}
	}

	indexData := IndexPageData{
		RecentFiles:  recentFiles,
		Tags:         tags,
		Content:      preambleContent,
		SiteName:     ctx.SiteName,
		BaseURL:      ctx.BaseURL,
		DefaultImage: ctx.DefaultImage,
		Author:       ctx.Author,
		LicenseName:  ctx.LicenseName,
		LicenseURL:   ctx.LicenseURL,
	}

	var outputBuf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&outputBuf, "index-page-template.html", indexData); err != nil {
		slog.Warn("Failed to execute index template", "error", err)
		result.Errors = 1
		return
	}

	if err := os.WriteFile(outputPath, outputBuf.Bytes(), 0644); err != nil {
		slog.Warn("Failed to write index page", "error", err)
		result.Errors = 1
		return
	}

	slog.Debug("Phase 3b complete: generated index page", "path", outputPath, "recent_files", len(recentFiles), "tags", len(tags))

	result.FilesGenerated = 1
	return
}

// GenerateAtomFeed creates an Atom feed with the most recent files.
// Writes output to ctx.DestDir/feed.xml. Returns a GenerationResult.
func GenerateAtomFeed(procFiles *ProcessedFiles, ctx BuildContext, tmpl *template.Template) (result GenerationResult) {
	slog.Debug("Starting Phase 3d: generating Atom feed")

	publicDir := ctx.DestDir
	outputPath := filepath.Join(publicDir, "feed.xml")

	if !ctx.ForceRebuild {
		if feedInfo, err := os.Stat(outputPath); err == nil {
			oldestFileTime := time.Now()
			for _, fi := range procFiles.Files {
				if fi.ModTime.Before(oldestFileTime) {
					oldestFileTime = fi.ModTime
				}
			}
			if !oldestFileTime.After(feedInfo.ModTime()) && !ctx.TmplModTime.After(feedInfo.ModTime()) {
				slog.Debug("Skipping Atom feed: cache valid")
				result.FilesSkipped = 1
				return
			}
		}
	}

	recentFiles := make([]FileInfo, 0, 20)
	if len(procFiles.Files) > 0 {
		sorted := make([]FileInfo, len(procFiles.Files))
		copy(sorted, procFiles.Files)
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].ModTime.After(sorted[j].ModTime)
		})
		if len(sorted) > 20 {
			recentFiles = sorted[:20]
		} else {
			recentFiles = sorted
		}
	}

	feedData := AtomFeedData{
		SiteName: ctx.SiteName,
		BaseURL:  ctx.BaseURL,
		Updated:  time.Now(),
		Files:    recentFiles,
		Author:   ctx.Author,
	}

	var outputBuf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&outputBuf, "atom-template.xml", feedData); err != nil {
		slog.Warn("Failed to execute Atom template", "error", err)
		result.Errors = 1
		return
	}

	if err := os.WriteFile(outputPath, outputBuf.Bytes(), 0644); err != nil {
		slog.Warn("Failed to write Atom feed", "error", err)
		result.Errors = 1
		return
	}

	slog.Debug("Phase 3d complete: generated Atom feed", "path", outputPath, "entries", len(recentFiles))

	result.FeedGenerated = true
	result.FilesGenerated = 1
	return
}

// CopyStaticFiles copies static assets from the static directory to the output directory.
// Returns a GenerationResult with counts of copied files and errors.
func CopyStaticFiles(_ *ProcessedFiles, ctx BuildContext) (result GenerationResult) {
	slog.Debug("Starting Phase 3c: copying static files")

	staticDir := filepath.Join(ctx.Root, "static")
	publicDir := ctx.DestDir

	entries, err := os.ReadDir(staticDir)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Debug("No static directory found, skipping", "path", staticDir)
			return
		}
		slog.Warn("Error reading static directory", "error", err)
		result.Errors = 1
		return
	}

	if err := os.MkdirAll(publicDir, 0755); err != nil {
		slog.Warn("Failed to create public directory", "error", err)
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
			slog.Warn("Failed to copy file", "name", entry.Name(), "error", err)
			result.Errors++
		} else {
			slog.Debug("Copied static file", "name", entry.Name())
			result.StaticFilesCopied++
		}
	}
	slog.Debug("Phase 3c complete", "files_copied", result.StaticFilesCopied, "errors", result.Errors)
	return
}
