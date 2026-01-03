package generator

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
)

// GetAndProcessOrgFiles walks absPath discovering .org files,
// then parses each to extract titles, tags, previews, last
// modification times, and UUIDs. Returns a ProcessedFiles
// containing all discovered files along with populated UuidMap
// and TagMap for cross-reference lookups, plus a GenerationResult.
func GetAndProcessOrgFiles(ctx BuildContext) (*ProcessedFiles, GenerationResult) {
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
				existing, _ := procFiles.TagMap.LoadOrStore(tag, []FileInfo{*fi})
				if existingSlice, ok := existing.([]FileInfo); ok {
					duplicate := false
					for _, f := range existingSlice {
						if f.Path == fi.Path {
							duplicate = true
							break
						}
					}
					if !duplicate {
						procFiles.TagMap.Store(tag, append(existingSlice, *fi))
					}
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
		slog.Debug("Skipping empty file", "path", filePath)
		return nil, nil
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	resultFI := &FileInfo{
		Path:    filePath,
		ModTime: info.ModTime(),
		Preview: extractPreview(data, 500),
		Title:   extractTitle(data),
		Tags:    extractTags(data),
		UUIDs:   extractUUIDs(data),
	}

	slog.Debug("Extracted file metadata",
		"path", filePath,
		"title", resultFI.Title,
		"tags", resultFI.Tags,
		"uuid_count", len(resultFI.UUIDs),
		"uuids", resultFI.UUIDs)

	for _, uuid := range resultFI.UUIDs {
		procFiles.UuidMap.Store("id:"+uuid, filePath)
	}

	return resultFI, nil
}

func extractTitle(orgContent []byte) string {
	s := string(orgContent)
	lines := strings.SplitSeq(s, "\n")

	for line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(trimmed), "#+title:") {
			title := strings.TrimSpace(trimmed[8:])
			if title != "" {
				slog.Debug("Found title in #+title: directive", "title", title)
				return title
			}
		} else if strings.HasPrefix(trimmed, "* ") && trimmed != "* " {
			titlePart := strings.TrimSpace(trimmed[2:])
			if spaceIdx := strings.Index(titlePart, " :"); spaceIdx != -1 {
				titlePart = strings.TrimSpace(titlePart[:spaceIdx])
			}
			if titlePart != "" {
				slog.Debug("Found title in headline", "title", titlePart)
				return titlePart
			}
		}
	}

	slog.Warn("No title found in org file")
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
				break
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
			if len(tags) > 0 {
				slog.Debug("Extracted tags from headline", "tags", tags)
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
	_, after, ok := strings.Cut(s, "\n")
	if !ok {
		return ""
	}

	contentAfterFirstLine := after

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
