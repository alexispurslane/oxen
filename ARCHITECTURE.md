# Architecture

Oxen is designed as a pipeline-based static site generator with clear separation of concerns and extensive use of concurrency for performance.

## Pipeline Architecture

Oxen's build process is implemented as a pipeline with multiple phases, all coordinated through a `BuildContext` that carries configuration and state:

### BuildContext

The `BuildContext` struct (in `generator/types.go`) is the central state container passed through all pipeline phases. It includes:
- File system paths (`Root`, `DestDir`)
- Build configuration (`ForceRebuild`, `TmplModTime`)
- Site configuration (`SiteName`, `BaseURL`, `Author`, `LicenseName`, `LicenseURL`, `DefaultImage`)

### Phase 1: Discovery and Parsing

**Location**: `generator/phase1.go`

Oxen walks your source directory to discover all `.org` files using `filepath.WalkDir`. Once discovered, each file is processed concurrently in parallel goroutines:

1. **File reading and parsing**: Each file is read and parsed using the `go-org` library
2. **Metadata extraction**: 
   - Title derived from filename (cleaned up)
   - Modification time from filesystem
   - Tags from `#+filetags:` or `:TAGS:` properties
   - UUIDs from `:ID:` properties in property drawers
3. **Preview generation**: Walks the org-mode AST to extract plain text content. The AST walker handles different node types appropriately - extracting text from `org.Text` nodes, link descriptions from `org.RegularLink` nodes (falling back to URLs if no description), etc.
4. **Index building**: 
   - `UuidMap`: Maps UUIDs to `HeaderLocation` (file path + header index) using `sync.Map`
   - `TagMap`: Maps tags to arrays of `FileInfo` structs using `sync.Map`

This phase uses goroutines and `sync.WaitGroup` for concurrent processing while maintaining thread-safe access to shared indexes.

### Phase 2: HTML Generation

**Location**: `generator/phase2.go`

With parsed files and the UUID index ready, Oxen generates HTML:

1. **UUID link resolution**: Implements a custom `org.Writer` that transforms `id:UUID` links during HTML generation:
   - `uuidReplacingWriter` embeds `org.HTMLWriter` and overrides `WriteRegularLink()`
   - As the AST is walked to generate HTML, each link is intercepted in real-time
   - Extracts UUID from `id:550e8400-e29b-41d4-a716-446655440000` format
   - Looks up target location in `UuidMap` and calculates relative path
   - Converts to relative path with anchor: `posts/my-file.html#headline-3`
   - This approach avoids text search or multiple phases by integrating directly into the HTML writing process
2. **Template execution**: Wraps content in templates with full config access via `PageData` struct
3. **Cache checking**: If neither source file nor templates have changed since last build, skips regeneration

### Phase 3: Aggregation

**Location**: `generator/phase3.go`

Generates supporting pages and assets:

**Tag Pages** (`GenerateTagPages`):
- Iterates through `TagMap` (thread-safe via `sync.Map.Range`)
- Each tag gets a page listing all files with that tag
- Files sorted by modification time (newest first)
- Generated concurrently with goroutines

**Index Page** (`GenerateIndexPage`):
- Shows 5 most recently modified files from `RecentFiles`
- Lists all tags with file counts
- Includes HTML content from `sitemap-preamble.org` if present

**Atom Feed** (`GenerateAtomFeed`):
- Generates `feed.xml` with 20 most recent files
- Proper Atom IDs using `BaseURL`
- Includes author information if configured

**Static Files** (`CopyStaticFiles`):
- Copies non-org files from `static/` directory
- Preserves directory structure
- Includes CSS, images, fonts, etc.

## Concurrency Model

Oxen uses goroutines extensively for I/O-bound and CPU-bound operations:

- **File discovery**: Sequential directory walking
- **File parsing**: Each file parsed and metadata-extracted concurrently
- **HTML generation**: Each file converted to HTML concurrently
- **Tag page generation**: Each tag page created in parallel

Shared data structures (`UuidMap`, `TagMap`) use `sync.Map` for thread-safe concurrent access without explicit locking.

## Template System

Templates use Go's `html/template` package with these features:

- **Custom functions**: `pathNoExt`, `formatRFC3339`, `sub`
- **Template inheritance**: `base-template.html` defines blocks that other templates override
- **Data access**: All config values passed through template data structs
- **Default templates**: Embedded in binary if no custom templates found

Template loading prioritizes:
1. Custom templates in `<source>/templates/`
2. Embedded default templates in `generator/templates/`

## ID Resolution System

The UUID resolution system is Oxen's unique feature:

1. **ID extraction**: During Phase 1, all `:ID:` properties are extracted and stored in `UuidMap`
2. **Link transformation**: During Phase 2, `id:UUID` links are intercepted and transformed
3. **Path calculation**: Relative paths calculated between source and target files
4. **Anchor generation**: Header indices used to create anchor links (`#headline-N`)

This enables hypertext networks that survive file/section renames and moves, supporting Zettelkasten and Ted Nelson-style hypertext approaches.

## Configuration Flow

1. **Load**: `config.LoadConfig()` reads `.oxen.json` or parses `--config` JSON
2. **Merge**: Command-line `--config` takes precedence over file
3. **Pass**: Config values flow through `BuildContext` to all pipeline phases
4. **Access**: Templates receive config values via data structs (PageData, etc.)

The config system supports arbitrary license configuration and site metadata without requiring code changes.
