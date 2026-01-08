package generator

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/niklasfasching/go-org/org"
)

// FindAndProcessOrgFiles walks absPath discovering .org files,
// then parses each to extract titles, tags, previews, last
// modification times, and UUIDs. Returns a ProcessedFiles
// containing all discovered files along with populated UuidMap
// and TagMap for cross-reference lookups, plus a GenerationResult.
func FindAndProcessOrgFiles(_ *ProcessedFiles, ctx BuildContext) (*ProcessedFiles, GenerationResult) {
	slog.Debug("Starting Phase 1: collecting and processing org files", "root", ctx.Root)
	files := collectOrgFiles(ctx.Root)
	slog.Debug("Collected org files", "count", len(files))

	procFiles := &ProcessedFiles{
		Files:   files,
		UuidMap: sync.Map{},
		TagMap:  sync.Map{},
	}

	var filesWithUUIDs int64
	var wg sync.WaitGroup
	for i := range files {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			fi, err := processFile(files[idx].Path, ctx.Root, procFiles)
			if err != nil {
				slog.Error("Error processing file", "path", files[idx].Path, "error", err)
				return
			}
			if fi == nil {
				return
			}
			files[idx] = *fi
			if len(fi.UUIDs) > 0 {
				atomic.AddInt64(&filesWithUUIDs, 1)
			}

			for _, tag := range fi.Tags {
				existing, _ := procFiles.TagMap.LoadOrStore(tag, []FileInfo{})
				if existingSlice, ok := existing.([]FileInfo); ok {
					procFiles.TagMap.Store(tag, append(existingSlice, *fi))
				}
			}
		}(i)
	}
	wg.Wait()

	slog.Debug("Phase 1 complete", "files_processed", len(files), "files_with_uuids", int(filesWithUUIDs))

	return procFiles, GenerationResult{
		TotalFilesScanned: len(files),
		FilesWithUUIDs:    int(filesWithUUIDs),
	}
}

func collectOrgFiles(root string) []FileInfo {
	slog.Debug("Scanning directory for .org files", "root", root)
	var files []FileInfo
	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && strings.HasSuffix(path, ".org") {
			info, err := d.Info()
			if err != nil {
				slog.Error("Error getting file info", "path", path, "error", err)
				return nil
			}
			relPath := strings.TrimPrefix(path, root+string(filepath.Separator))
			if relPath == path {
				relPath = strings.TrimPrefix(path, root)
			}
			slog.Debug("Found .org file", "path", relPath, "mod_time", info.ModTime())
			files = append(files, FileInfo{
				Path:    relPath,
				ModTime: info.ModTime(),
			})
		}
		return nil
	})
	return files
}

func processFile(filePath, root string, procFiles *ProcessedFiles) (*FileInfo, error) {
	absPath := filepath.Join(root, filePath)
	slog.Debug("Processing org file", "path", filePath)

	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	if len(data) == 0 {
		slog.Debug("Skipping empty file", "path", filePath)
		return nil, nil
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	conf := org.New()
	doc := conf.Parse(bytes.NewReader(data), absPath)

	resultFI := &FileInfo{
		Path:      filePath,
		ModTime:   info.ModTime(),
		Preview:   extractPreviewFromAST(doc, 500),
		Title:     extractTitleFromAST(doc),
		Tags:      extractTagsFromAST(doc),
		UUIDs:     extractUUIDsFromAST(doc),
		ParsedOrg: doc,
	}

	slog.Debug("Extracted file metadata",
		"path", filePath,
		"title", resultFI.Title,
		"tags", resultFI.Tags,
		"uuid_count", len(resultFI.UUIDs),
		"uuids", resultFI.UUIDs)

	for _, uuid := range resultFI.UUIDs {
		procFiles.UuidMap.Store(uuid, filePath)
	}

	return resultFI, nil
}

func extractTitleFromAST(doc *org.Document) string {
	if title := doc.Get("TITLE"); title != "" {
		slog.Debug("Found title in #+TITLE: directive", "title", title)
		return title
	}
	if title := doc.Get("title"); title != "" {
		slog.Debug("Found title in #+title: directive", "title", title)
		return title
	}

	for _, node := range doc.Nodes {
		if headline, ok := node.(org.Headline); ok {
			title := org.String(headline.Title...)
			if title != "" {
				slog.Debug("Found title in headline", "title", title)
				return title
			}
		}
	}

	slog.Warn("No title found in org file")
	return ""
}

func extractTagsFromAST(doc *org.Document) []string {
	for _, node := range doc.Nodes {
		if headline, ok := node.(org.Headline); ok {
			if len(headline.Tags) > 0 {
				slog.Debug("Extracted tags from headline", "tags", headline.Tags)
				return headline.Tags
			}
		}
	}
	return nil
}

func extractUUIDsFromAST(doc *org.Document) []string {
	var uuids []string
	seen := make(map[string]bool)

	for _, node := range doc.Nodes {
		if headline, ok := node.(org.Headline); ok {
			if headline.Properties != nil {
				// Iterate through all properties to find multiple ID entries
				for _, prop := range headline.Properties.Properties {
					if prop[0] == "ID" && prop[1] != "" {
						id := prop[1]
						if isValidUUID(id) && !seen[id] {
							uuids = append(uuids, id)
							seen[id] = true
						}
					}
				}
			}
		}
	}

	if len(uuids) > 0 {
		slog.Debug("Extracted UUIDs from property drawers", "uuids", uuids)
	}
	return uuids
}

func extractPreviewFromAST(doc *org.Document, maxLen int) string {
	var builder strings.Builder

	// Collect text content, stopping early when approaching maxLen
	var collectText func(org.Node) bool
	collectText = func(node org.Node) bool {
		if builder.Len() >= maxLen {
			return false
		}
		switch n := node.(type) {
		case org.Text:
			builder.WriteString(n.Content)
		case org.RegularLink:
			if len(n.Description) > 0 {
				builder.WriteString(strings.TrimSpace(org.String(n.Description...)))
			} else {
				builder.WriteString(n.URL)
			}
		case org.Emphasis:
			builder.WriteString(strings.TrimSpace(org.String(n.Content...)))
		case org.Block:
			for _, child := range n.Children {
				if !collectText(child) {
					return false
				}
			}
			builder.WriteString(" ")
		default:
			if children := getChildren(node); children != nil {
				for _, child := range children {
					if !collectText(child) {
						return false
					}
				}
			}
		}
		return builder.Len() < maxLen
	}

	for _, node := range doc.Nodes {
		if !collectText(node) {
			break
		}
	}

	text := builder.String()

	re := regexp.MustCompile(`([.!?])([A-Za-z])`)
	text = re.ReplaceAllString(text, "$1 $2")

	text = strings.Join(strings.Fields(text), " ")

	// Make sure it's exactly right
	if len(text) > maxLen {
		cutAt := maxLen
		for cutAt > 0 && text[cutAt-1] > 127 {
			cutAt--
		}
		return text[:cutAt] + "..."
	}

	return text
}

// Helper to get children from different node types
func getChildren(node org.Node) []org.Node {
	switch n := node.(type) {
	case org.Paragraph:
		return n.Children
	case org.Headline:
		return n.Children
	case org.Block:
		return n.Children
	default:
		return nil
	}
}
