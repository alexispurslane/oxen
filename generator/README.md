# Generator Package

## Files

- `types.go` - Core data types and structures
- `phase1.go` - File discovery and org-mode metadata parsing/extraction
- `phase2.go` - Template loading and HTML generation
- `phase3.go` - Index and tag pages and static file handling
- `utils.go` - Helper functions for UUID extraction and file copying
- `templates/` - Embedded HTML templates
  - `base-template.html` - Base layout template
  - `page-template.html` - Individual page template
  - `tag-page-template.html` - Tag listing page template
  - `index-page-template.html` - Sitemap template

## Purpose

The `generator` package contains the core static site generation logic for Oxen. It handles parsing org-mode files, extracting metadata, resolving UUID references, applying templates, and generating the complete HTML output.
