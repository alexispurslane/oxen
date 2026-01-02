

# Code Review: Oxen Static Site Generator

## Project Overview

This is an Org-mode to HTML static site generator with watch mode, a dev server (SSE-based hot reload), tag pages, and UUID link resolution. The code is ~1,100 lines across 8 files. For a personal project with decades-long horizon, I identified several maintainability concerns.

---

## Critical Concerns

### 1. External Dependency Abandonment Risk

**`go.mod:6-8`**, **`phase2.go:15-16`**

```go
require (
    github.com/anknown/ahocorasick v0.0.0-20190904063843-d75dbd5169c0
    github.com/niklasfasching/go-org v1.9.1
)
```

Both dependencies are critical and unmaintained:
- `go-org` hasn't been updated since 2022
- `ahocorasick` is a tiny abandoned repo

**Risk**: If these break with future Go versions or have security issues, you have no path forward without rewriting core functionality.

**Recommendation**: Fork/vendor these dependencies now, or write minimal replacement code. The UUID link replacement (`phase2.go:211-269`) is only ~60 lines and could be replaced with standard library regex or a simple string search.

---

### 2. Global Mutable State

**`types.go:47`** and throughout

```go
var stats BuildStats
```

The `stats` struct is globally mutable and accessed concurrently via atomic operations. This pattern makes testing harder and introduces subtle bugs.

**Issues**:
- `GetStats()` returns a pointer to global state (`types.go:28-30`)
- `Reset()` mutates global state (`types.go:19-26`)
- Stats are printed at the end but errors only logged, not returned

**Recommendation**: Pass stats as a parameter or use a struct with methods. Consider making the generator a configurable struct:

```go
type Generator struct {
    Stats *Stats
    Workers int
}
```

---

### 3. Race Conditions and Type Safety

**`phase2.go:106-110`**, **`phase3.go:18-21`**, **`phase3.go:84-91`**

Type assertions on `sync.Map` values lack panic safety:

```go
procFiles.UuidMap.Range(func(key, value any) bool {
    keywords = append(keywords, key.(string))      // unsafe
    replacements = append(replacements, value.(string))  // unsafe
    return true
})
```

**Recommendation**: Use type switch with fallthrough or helper functions:

```go
if str, ok := value.(string); ok {
    replacements = append(replacements, str)
} else {
    log.Printf("unexpected type in UuidMap: %T", value)
}
```

---

## Moderate Concerns

### 4. Inconsistent Worker Usage

**`phase1.go:19-20`** vs **`phase1.go:29-36`**

```go
numWorkers := min(workers, len(files))
// ...
for chunk := range filesToSpans(files, numWorkers) {
```

In Phase 1, workers are capped at file count. In Phase 2 (`phase2.go:115-124`), the full worker count is used as goroutine count, but a channel buffer of `workers*2` is created. The first pass may spawn more goroutines than intended.

---

### 5. Memory-Mapped Files for Small Org Files

**`phase1.go:114-118`**

```go
data, err := syscall.Mmap(int(file.Fd()), 0, int(size), syscall.PROT_READ, syscall.MAP_PRIVATE)
```

Using `mmap` for potentially small org files adds complexity without benefit. It requires `Munmap` defer and handles `syscall` differently on Windows.

**Recommendation**: Use `os.ReadFile()` which is simpler and handles all file sizes.

---

### 6. Server SSE Client Management

**`server.go:50`**

```go
s.clients.Store(r.RemoteAddr, client)
```

Using `RemoteAddr` as a key is unreliable—multiple browser tabs share the same address, and addresses can be spoofed or change.

**Impact**: Reload notifications may not work correctly in some browsers, or may disconnect wrong clients.

---

### 7. Hardcoded Site Name in Templates

**`templates/base-template.html:6,12,24`** and **`templates/index-page-template.html:5`**

The site name "Neon Vagabond" is hardcoded in 4 places. If you ever want to reuse this tool or change the site name, you must grep for all occurrences.

**Recommendation**: Pass site name via template data struct, or use a simple config file.

---

### 8. Magic Numbers Without Constants

**`phase1.go:223`** and throughout

```go
if len(contentAfterFirstLine) > maxLen {  // maxLen is a parameter, good
    cutAt := maxLen
    for cutAt > 0 && contentAfterFirstLine[cutAt-1] > 127 {  // 127 = ASCII DELETE
```

The `127` check for non-ASCII byte detection is cryptic. Also:
- `500` preview length (`phase1.go:23`)
- `100 * time.Millisecond` debounce (`main.go:173`)
- `8` default workers (`main.go:267,272,276`)

---

### 9. Incomplete Incremental Build Logic

**`main.go:28-36`**

```go
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
```

This checks if the dest directory is empty, but doesn't verify if the *contents* match the source. If you delete a source file, the old HTML remains. Consider tracking a manifest file.

---

### 10. Error Handling Inconsistencies

**`phase3.go:98-103`** silently ignores preamble errors:

```go
if data, err := os.ReadFile(preamblePath); err == nil {
    if htmlContent, err := convertOrgToHTML(data, "sitemap-preamble.org"); err == nil {
        preambleContent = template.HTML(htmlContent)
    }
}
```

No logging if the preamble is missing or malformed.

---

## Minor Issues

### 11. Template Parsing Duplication

**`phase2.go:37-96`**: The `useFS` branch parses each template separately from the base, but the non-FS branch also does this. Consider extracting common logic.

### 12. No Tests

No test files exist. For a project you intend to use for decades, even basic regression tests prevent silent breakage.

### 13. `filepath.Separator` Usage

**`phase1.go:52`**: Uses `string(filepath.Separator)` when `path/filepath` already provides `pathSeparator` or you could just use `/` since Go normalizes paths on all platforms.

---

## Summary Table

| Category | Severity | Files |
|----------|----------|-------|
| Dependency abandonment risk | Critical | go.mod, phase2.go |
| Global mutable state | High | types.go, generator/*.go |
| Race conditions | High | phase2.go, phase3.go |
| SSE client keying | Medium | server.go |
| Hardcoded values | Medium | templates/, main.go |
| mmap for small files | Low | phase1.go |

---

## Recommendations for Decades of Use

1. **Vendor dependencies** now while they work
2. **Add a `go.mod` pin** for exact versions
3. **Write replacement code** for the two external libs (~100 lines total)
4. **Convert stats to a struct** passed through call chain
5. **Add basic tests** for org parsing and UUID replacement
6. **Extract magic numbers** to named constants
7. **Create a `config.yaml`** for site name and other settings
