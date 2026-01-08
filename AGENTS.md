# OpenCode Project Configuration

This file provides guidance for AI assistants working with the oxen codebase.

## Project Overview

**oxen** is a static site generator specifically designed for org-mode files. It converts `.org` files to HTML with features like tag-based organization, UUID-based cross-references, and concurrent processing.

## Technology Stack

- **Language**: Go 1.25+
- **Parser**: `github.com/niklasfasching/go-org` for org-mode parsing
- **Concurrency**: `sync.Map`, goroutines, `sync.WaitGroup`
- **Templates**: `html/template` with embedded templates
- **CLI**: `github.com/spf13/cobra` for command-line interface
- **Testing**: Standard `testing` package with custom helpers
- **Logging**: `log/slog` for structured logging
- **Terminal**: `fatih/color` for colored output

## Project Structure

```
oxen/
├── generator/              # Core generation logic
│   ├── templates/          # HTML templates (embedded)
│   ├── types.go           # Data structures and types
│   ├── phase1.go          # File discovery and org parsing
│   ├── phase2.go          # HTML generation with UUID link resolution
│   ├── phase3.go          # Tag/index page generation
│   ├── utils.go           # File operations and UUID validation
│   ├── *_test.go          # Unit tests
├── generator_test/        # End-to-end integration tests
│   └── integration_test.go
├── server/                # HTTP server (minimal)
├── vendor/                # Vendored dependencies
├── go.mod                 # Module definition
└── main.go               # CLI entry point
```

## Testing

### Test Types

1. **Unit Tests** (`generator/*_test.go`)
   - Test individual functions in isolation
   - Use temp directories for file operations
   - Focus on specific functionality

2. **Integration Tests** (`generator_test/integration_test.go`)
   - End-to-end testing of phases working together
   - Test actual file generation and HTML output
   - Verify ID link resolution across directories

### Running Tests

```bash
# Run all tests
go test ./...

# Run with coverage
go test -cover ./...

# Run specific package
go test ./generator
go test ./generator_test

# Run specific test
go test -v ./generator -run TestProcessFile
go test -v ./generator_test -run TestE2E_Phases1And2_IDLinkResolution

# Verbose output
go test -v ./...
```

### Test Utilities

The codebase provides test helpers in `generator/utils_test.go`:
- `MustCreateTempDir(t, prefix)` - Create temp directories safely
- `CreateTestFileWithModTime()` - Create test files with specific timestamps
- `CreateTestOrgFile()` - Create org files with content

## Code Style Guidelines

### Type Definitions

**Use domain-specific types** for clarity and type safety:

```go
// Good - domain-specific types
type UUID string
type HeaderIndex int
type UUIDMap map[UUID]HeaderIndex

// Avoid - raw primitives without context
func process(id string, index int)
```

**Document the purpose** of each type:

```go
// UUIDMap maps UUID strings to their header indices within a file.
type UUIDMap map[UUID]HeaderIndex
```

### Concurrency

**Always use sync.Map for concurrent access** to shared data structures:

```go
type ProcessedFiles struct {
    Files   []FileInfo
    UuidMap sync.Map  // UUID → HeaderLocation
    TagMap  sync.Map  // Tag → []FileInfo
}
```

**Process files concurrently** when I/O bound or CPU bound:

```go
var wg sync.WaitGroup
for _, fi := range procFiles.Files {
    wg.Add(1)
    go func(fi FileInfo) {
        defer wg.Done()
        // process file
    }(fi)
}
wg.Wait()
```

### Error Handling

**Handle errors immediately** and return early:

```go
result, err := processFile(path, root, procFiles)
if err != nil {
    return nil, err  // Early return
}
// Continue with successful result
```

**Use structured logging** for warnings and errors:

```go
slog.Warn("Warning: unexpected type in UuidMap key", "key", key, "type", fmt.Sprintf("%T", key))
slog.Debug("Processing file", "path", filePath, "uuid_count", len(uuids))
```

### Function Organization

**Separate concerns into phases**:

```go
// Phase 1: Discovery and parsing
func FindAndProcessOrgFiles(...) (*ProcessedFiles, GenerationResult)

// Phase 2: HTML generation
func GenerateHtmlPages(...) GenerationResult

// Phase 3: Tag/index generation
func GenerateTagAndIndexPages(...) GenerationResult
```

**Each phase should**:
- Accept `*ProcessedFiles` and `BuildContext`
- Return appropriate results
- Be independently testable

### Data Structures

**Use structs for complex return values**:

```go
type GenerationResult struct {
    TotalFilesScanned int
    FilesGenerated    int
    Errors            int
    // ...
}
```

**Provide aggregation methods**:

```go
func (r GenerationResult) Add(other GenerationResult) GenerationResult {
    return GenerationResult{
        TotalFilesScanned: r.TotalFilesScanned + other.TotalFilesScanned,
        // ...
    }
}
```

### Testing Patterns

**Always clean up temp resources**:

```go
tmpDir := MustCreateTempDir(t, "test-")
defer CleanupTempDir(tmpDir)
```

**Test one concept per test**:

```go
func TestExtractUUIDs(t *testing.T) {
    tests := []struct {
        name     string
        content  string
        expected UUIDMap
    }{
        // Each test case is independent
    }
    // ...
}
```

**Use table-driven tests** for multiple scenarios.

### org-mode Integration

**Work with go-org's AST** directly:

```go
doc := conf.Parse(bytes.NewReader(content), "test.org")
// Traverse nodes
doc.Walk(func(n org.Node) {
    if headline, ok := n.(org.Headline); ok {
        // Process headline
    }
})
```

**Use custom writers** for link transformation:

```go
type uuidReplacingWriter struct {
    *org.HTMLWriter
    uuidToPath map[UUID]HeaderLocation
}

func (w *uuidReplacingWriter) WriteRegularLink(link org.RegularLink) {
    // Transform id: links
}
```

### Code Comments

**Document the "why"**, not the "what":

```go
// Good - explains purpose
// UUIDMap maps UUID strings to their header indices within a file.
type UUIDMap map[UUID]HeaderIndex

// Avoid - redundant
// This is a map that maps strings to ints
type MyMap map[string]int
```

**Explain complex logic**:

```go
// Build relative path between current file and target file
currentDir := filepath.Dir(w.currentPath)
targetDir := filepath.Dir(targetPath.FilePath)
relPath, _ := filepath.Rel(currentDir, targetDir)
```

### Naming Conventions

- **Exported types/functions**: PascalCase (`FileInfo`, `GenerateHtmlPages`)
- **Unexported**: camelCase (`extractUUIDsFromAST`)
- **Acronyms**: All caps (`UUID`, `HTML`)
- **Test functions**: `Test` + PascalCase subject (`TestProcessFile`)

### Import Organization

Group imports as:
1. Standard library
2. Third-party
3. Internal packages

```go
import (
    "bytes"
    "os"
    "path/filepath"
    "strings"
    "testing"
    "time"

    "github.com/niklasfasching/go-org/org"

    "oxen/generator"
)
```

## Common Patterns

### UUID Link Resolution

The system transforms `id:UUID` links to relative HTML paths with anchors:

```
id:550e8400-e29b-41d4-a716-446655440001 → doc1.html#headline-1
```

### Pipeline Execution

```go
pipeline := generator.NewPipeline(ctx)
result := pipeline.
    WithFullPhase(generator.FindAndProcessOrgFiles).
    WithOutputOnlyPhase(func(pf *generator.ProcessedFiles, ctx generator.BuildContext) generator.GenerationResult {
        // Custom phase
        return generator.GenerationResult{}
    }).
    Execute()
```

### Template Processing

Templates are embedded and use helper functions:

```go
// In SetupTemplates
"pathNoExt": func(path string) string {
    return strings.TrimSuffix(path, ".org")
}
```

## Performance Considerations

- **Concurrent file processing** - All org files are processed in parallel
- **sync.Map** - Designed for concurrent read/write access patterns
- **Cache checking** - Skip regeneration when source files haven't changed
- **No global state** - Everything passed through structs/contexts

## Error Handling Philosophy

- **Fail fast** - Return errors immediately when encountered
- **Log and continue** - Warn about issues but finish processing (e.g., invalid UUIDs)
- **Graceful degradation** - Skip individual files but complete overall generation
- **Clear error messages** - Include context (file path, UUID, etc.)

This codebase prioritizes **type safety**, **concurrency correctness**, and **clear domain modeling** over clever shortcuts. When in doubt, prefer explicit types and verbose documentation.
