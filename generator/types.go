package generator

import (
	"embed"
	"fmt"
	"html/template"
	"regexp"
	"sort"
	"sync"
	"time"
)

type BuildContext struct {
	Root         string
	DestDir      string
	Workers      int
	ForceRebuild bool
	TmplModTime  time.Time
}

type ProcessedFiles struct {
	Files   []FileInfo
	UuidMap sync.Map
	TagMap  sync.Map
}

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

type GenerationResult struct {
	TotalFilesScanned int
	FilesWithUUIDs    int
	FilesGenerated    int
	FilesSkipped      int
	TagPagesGenerated int
	StaticFilesCopied int
	Errors            int
	startTime         time.Time
}

func (r GenerationResult) Add(other GenerationResult) GenerationResult {
	return GenerationResult{
		TotalFilesScanned: r.TotalFilesScanned + other.TotalFilesScanned,
		FilesWithUUIDs:    r.FilesWithUUIDs + other.FilesWithUUIDs,
		FilesGenerated:    r.FilesGenerated + other.FilesGenerated,
		FilesSkipped:      r.FilesSkipped + other.FilesSkipped,
		TagPagesGenerated: r.TagPagesGenerated + other.TagPagesGenerated,
		StaticFilesCopied: r.StaticFilesCopied + other.StaticFilesCopied,
		Errors:            r.Errors + other.Errors,
	}
}

func (r GenerationResult) PrintSummary(procFiles *ProcessedFiles) {
	duration := time.Now().Sub(r.startTime)

	type tagCount struct {
		name  string
		count int
	}
	var tags []tagCount
	procFiles.TagMap.Range(func(key, value any) bool {
		if tag, ok := key.(string); ok {
			if files, ok := value.([]FileInfo); ok {
				tags = append(tags, tagCount{name: tag, count: len(files)})
			}
		}
		return true
	})
	sort.Slice(tags, func(i, j int) bool {
		return tags[i].name < tags[j].name
	})

	fmt.Printf("\n=== Generation Complete ===\n")
	fmt.Printf("Total files scanned:    %d\n", r.TotalFilesScanned)
	fmt.Printf("Files with UUIDs:       %d\n", r.FilesWithUUIDs)
	fmt.Printf("Files generated:        %d\n", r.FilesGenerated)
	fmt.Printf("Files skipped:          %d\n", r.FilesSkipped)
	fmt.Printf("Tag pages generated:    %d\n", r.TagPagesGenerated)
	fmt.Printf("Static files copied:    %d\n", r.StaticFilesCopied)
	fmt.Printf("Tags (%d):              ", len(tags))
	for i, tc := range tags {
		if i > 0 {
			fmt.Printf(", ")
		}
		fmt.Printf("%s (%d)", tc.name, tc.count)
	}
	fmt.Printf("\n")
	fmt.Printf("Errors:                 %d\n", r.Errors)
	fmt.Printf("Duration:               %v\n", duration.Round(time.Millisecond))
}

func (r *GenerationResult) SetStartTime(t time.Time) {
	r.startTime = t
}
