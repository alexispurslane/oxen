package generator

import (
	"embed"
	"fmt"
	"html/template"
	"regexp"
	"sort"
	"sync"
	"time"

	"github.com/fatih/color"
)

type BuildContext struct {
	Root         string
	DestDir      string
	ForceRebuild bool
	TmplModTime  time.Time
	SiteName     string
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
	reBlocks          = regexp.MustCompile(`(?m)^\s*#\+begin_\S+[\s\S]*?^\s*#\+end_\S+`)
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
	Content  template.HTML
	SiteName string
}

type TagPageData struct {
	Title    string
	Files    []FileInfo
	SiteName string
}

type TagInfo struct {
	Name  string
	Count int
}

type IndexPageData struct {
	RecentFiles []FileInfo
	Tags        []TagInfo
	Content     template.HTML
	SiteName    string
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
		return tags[i].count > tags[j].count
	})

	pastelMagenta := color.RGB(255, 182, 193).SprintFunc()
	pastelBlue := color.RGB(173, 216, 230).SprintFunc()
	pastelGreen := color.RGB(152, 251, 152).SprintFunc()
	pastelRed := color.RGB(255, 160, 160).SprintFunc()
	pastelYellow := color.RGB(255, 255, 224).SprintFunc()

	fmt.Printf("\n✨  %s  ✨\n\n", pastelMagenta("Generation Complete!"))
	fmt.Printf("Total files scanned:  %s\n", pastelBlue(r.TotalFilesScanned))
	fmt.Printf("Files with UUIDs:     %s\n", pastelBlue(r.FilesWithUUIDs))
	fmt.Printf("Files generated:      %s\n", pastelGreen(r.FilesGenerated))
	fmt.Printf("Files skipped:        %s\n", pastelBlue(r.FilesSkipped))
	fmt.Printf("Tag pages generated:  %s\n", pastelGreen(r.TagPagesGenerated))
	fmt.Printf("Static files copied:  %s\n", pastelGreen(r.StaticFilesCopied))

	if r.Errors > 0 {
		fmt.Printf("Errors:               %s\n", pastelRed(r.Errors))
	} else {
		fmt.Printf("Errors:               %s\n", pastelGreen(0))
	}

	fmt.Printf("Duration:             %s\n", pastelYellow(duration.Round(time.Millisecond)))

	if len(tags) > 0 {
		pastelTagColors := []*color.Color{
			color.RGB(255, 182, 193),
			color.RGB(221, 160, 221),
			color.RGB(173, 216, 230),
			color.RGB(152, 251, 152),
			color.RGB(255, 228, 181),
			color.RGB(255, 255, 224),
		}

		fmt.Printf("\nTags (%d):\n", len(tags))
		for i := 0; i < len(tags); i += 3 {
			for j := 0; j < 3 && i+j < len(tags); j++ {
				tc := tags[i+j]
				hash := 0
				for _, c := range tc.name {
					hash = (hash*31 + int(c)) % len(pastelTagColors)
				}
				colorFunc := pastelTagColors[hash].SprintFunc()
				fmt.Printf(" %s", colorFunc(fmt.Sprintf("%s (%d)", tc.name, tc.count)))
			}
			fmt.Println()
		}
	}
}

func (r *GenerationResult) SetStartTime(t time.Time) {
	r.startTime = t
}

type Pipeline struct {
	ctx       BuildContext
	procFiles *ProcessedFiles
	result    GenerationResult
	phases    []func(*Pipeline) (*ProcessedFiles, GenerationResult)
}

func NewPipeline(ctx BuildContext) *Pipeline {
	return &Pipeline{
		ctx:    ctx,
		phases: []func(*Pipeline) (*ProcessedFiles, GenerationResult){},
		result: GenerationResult{},
	}
}

// WithFullPhase adds a phase that processes and potentially modifies procFiles
func (p *Pipeline) WithFullPhase(phase func(*ProcessedFiles, BuildContext) (*ProcessedFiles, GenerationResult)) *Pipeline {
	p.phases = append(p.phases, func(pl *Pipeline) (*ProcessedFiles, GenerationResult) {
		return phase(pl.procFiles, pl.ctx)
	})
	return p
}

// WithOutputOnlyPhase wraps a phase that only returns GenerationResult
func (p *Pipeline) WithOutputOnlyPhase(phase func(*ProcessedFiles, BuildContext) GenerationResult) *Pipeline {
	p.phases = append(p.phases, func(pl *Pipeline) (*ProcessedFiles, GenerationResult) {
		return pl.procFiles, phase(pl.procFiles, pl.ctx)
	})
	return p
}

func (p *Pipeline) Execute() (*ProcessedFiles, GenerationResult) {
	startTime := time.Now()

	for _, phase := range p.phases {
		var newResult GenerationResult
		p.procFiles, newResult = phase(p)
		p.result = p.result.Add(newResult)
	}

	p.result.SetStartTime(startTime)
	return p.procFiles, p.result
}
