# Oxen: Org-mode Static Site Generator

## Overview
Oxen is a high-performance, concurrent Go program that converts a collection of Org-mode files into a static HTML website. It generates cross-references between files using UUID-based linking and builds dynamic navigation pages.

## Requirements

### Functional Requirements
1. **Concurrent Directory Walking**: Recursively discover all `.org` files in the current directory
2. **UUID Extraction**: Extract UUIDs from property drawers for cross-referencing
3. **UUID Replacement**: Replace UUID references with org-mode hyperlinks to the files that define those UUIDs
4. **HTML Conversion**: Convert Org-mode to HTML using `go-org`
5. **Template Rendering**: Apply HTML template wrapper to generated pages
6. **Sitemap Generation**: Create dynamic index pages with recent files and tag listings

### Non-Functional Requirements
1. **Performance**: Must handle large org file collections efficiently through concurrency
2. **Memory Safety**: Use memory-safe concurrent data structures
3. **Correctness**: Exact matching for UUIDs (8-4-4-4-12 hexadecimal format)

## Architecture

### Phase 1: File Discovery (Parallel with Phase 2)
- Recursively walk directory tree starting from current working directory; walk sibling directories concurrently
- Send any `.org` files found over a channel to Phase 2

### Phase 2: UUID Extraction (Parallel with Phase 1)
- Process each `.org` file (**as they are recieved over a channel from phase 1**) by memory-mapping as bytes
- Scan linearly for `:ID: ` pattern (5-byte sequence)
- For each match, validate subsequent 36 bytes against UUID format:
  - 8 hex characters
  - hyphen
  - 4 hex characters  
  - hyphen
  - 4 hex characters
  - hyphen
  - 4 hex characters
  - hyphen
  - 12 hex characters
- Store in concurrent map: `"id:" + UUID` → relative file path

### Phase 3: HTML Generation
- Memory-map each `.org` file (if not `sitemap-preamble.org`)
- Use `ahocorasick` library to find all UUID references from Phase 2 map's keys
- Replace each `"id:" + UUID` from the map with `file:relative/path` to switch the org links from `id:` links to `file:` links without complex parsing
- Convert modified Org content to HTML using `go-org`
- Render through `html/template` using `page-template.html`
- Write output to `<file>.html`

### Phase 4: Sitemap Generation
- Load `sitemap-preamble.org`
- **Recent Files Section**:
  - Identify 5 most recently modified `.org` files (excluding `sitemap-preamble.org`)
  - Generate org-mode links to their HTML counterparts
  - Extract first ~500 characters of content (strip special chars) for blurbs
- **Tag Index Section**:
  - Find all `tag-*.org` files
  - Generate org-mode links to their HTML counterparts
  - Count links within each tag file
- Convert final sitemap to HTML

## Data Structures

### UUIDMap
```go
var uuidMap sync.Map  // "id:" + UUID => relative file path
```

### FileInfo
```go
type FileInfo struct {
    Path      string
    ModTime   time.Time
    Preview   string  // First 500 chars
}

var fileChan = make(chan FileInfo)
```

## Concurrency Model

1. **Worker Pool Pattern**: Fixed number of goroutines processing files from channel
2. **Fan-In**: Multiple workers concurrently write to shared data structures
3. **Parallel Phases**: Phase 1 and Phase 2 run concurrently, connected by channel
4. **WaitGroups**: Ensure phases complete before proceeding to next stage
5. **sync.Map**: Thread-safe access for UUIDMap

### Phases Execution
```
Phase 1 (File Discovery) ────┐
      │                      ├─> Barrier (both must finish)
      └─> channel            │
            │                │
            v                │
Phase 2 (UUID Extraction) ───┘
            │
            v
         Barrier
            │
            v
Phase 3 (HTML Generation)
            │
            v
         Barrier
            │
            v                             
Phase 4 (Sitemap)
```

## Algorithm Details

### UUID Validation State Machine
```
State 0: Looking for ':ID: ' (bytes 0x3A 0x49 0x44 0x3A 0x20)
State 1-8:   Collect 8 hex digits
State 9:     Expect hyphen
State 10-13: Collect 4 hex digits  
State 14:    Expect hyphen
State 15-18: Collect 4 hex digits
State 19:    Expect hyphen
State 20-23: Collect 4 hex digits
State 24:    Expect hyphen
State 25-36: Collect 12 hex digits
State 37:    Success - emit UUID, reset to State 0
```
Any mismatch → reset to State 0

### Aho-Corasick Setup
- Build automaton from all UUID map keys (prefixed with `"id:"`)
- For each match in file content, replace with `[[file:<path>][<id>]]`
- Use efficient multi-pattern matching for large substitution sets

## Dependencies

```
github.com/alecthomas/chroma/v2 v2.5.0       // Syntax highlighting (go-org)
github.com/anknown/ahocorasick v0.0.0-20190904063843-d75dbd5169c0  // Pattern matching
github.com/anknown/darts v0.0.0-20151216065714-83ff685239e6       // Double-array trie
github.com/niklasfasching/go-org v1.9.1      // Org-mode to HTML conversion
```

## File Outputs

- Each `<name>.org` → `<name>.html` (except `sitemap-preamble.org`)
- `sitemap.html` containing:
  - Recent files with blurbs
  - Tag index with link counts

## Error Handling Strategy

1. **Corrupted Files**: Log warning, skip file, continue processing
2. **Invalid UUIDs**: Ignore, don't add to map
3. **Missing Templates**: Fatal error - required for HTML generation
4. **Permission Errors**: Log error, skip file
5. **Concurrent Access**: Use mutexes to prevent race conditions
