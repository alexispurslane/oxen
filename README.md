# Oxen

*A simple, fast, standalone binary org-mode static site generator, built for hypertext enthusiasts and designed to last*

Think of it as a much simpler alternative to Hugo or Jekyll, or even Zola — designed specifically for those who live and breathe Emacs org-mode, but find `org-publish` too slow, or not portable enough (for instance, running headless in a CI system or in a systemd unit on your immutable core server).

What makes Oxen different: 

1. [Simple well known templating system](https://pkg.go.dev/html/template) and configuration language (JSON)
2. **Standalone Binary**: Compiles to a single, small, completely standalone binary (not even a dependency on libc!) which is trivially cross-compiled, making setup and installation a cinch, even on devices where you don't have the ability to install packages or compile things directly.
3. **Built to Last**: Built with completely vendored dependencies and no external build dependencies (e.g. C libraries): Oxen should be around for the long haul, because if you're using it — as I am — as your main way of expressing yourself to the world, then you need to be able to rely on it; vendored dependencies mean that even if I stop maintaining it, it will continue to work, since libraries and runtimes won't shift out from under it.
4. **Permissive License**: Licensed under the 0BSD, you can be confident that there are no restrictions on how you use Oxen for your own personal websites and content, as it should be. It's truly yours to do whatever you want with.
5. **Support for `org-mode`'s global ID system**: Other static site generators might be able to parse org syntax, but they have no concept of the `:ID:` properties that org-mode uses for true [Ted Nelson-esque](https://hyperland.com/TedCompOneLiners) location and name invariant cross-referencing of whole files or portions of them. When you build a site with Oxen, those UUIDs are actually understood, building an index of where they are defined and then resolving them before HTML export, making it possible to maintain hypertext networks that survive the movement and renaming of files or even sections within those files.
6. **Near-Instant Feedback**: a full from-scratch rebuild of a 140-file, 700-UUID, 700,000-word project takes about 500 milliseconds. When you combine that performance with watch mode and the built-in dev server with live reload, you get an instant feedback loop for writing and editing. Save a file, and within half a second at most (it also has incremental builds) your browser refreshes with the changes.
7. An aspiration to be **[Finished Software](https://josem.co/the-beauty-of-finished-software/)**: Oxen, at some point soon, should be done with a capital-D. There are only so many features you need in an SSG for org-mode second brains, really!

## What it does

Org-mode has built-in support for UUIDs as global identifiers for entries through the built in `org-id.el` package. These are stored as `:ID:` properties in your org files and serve as automatically generated filesystem-location and section-title invariant references to content. Instead of linking to files by their path in the filesystem hierarchy, or sections by title name, you link to content by these persistent IDs. If you move or rename a file or a section, the links still work because the ID travels with the entry.

This approach is particularly useful if you practice Zettelkasten, take a classic hypertext perspective à la Ted Nelson, or simply prefer an information architecture that doesn't rely on rigid file hierarchies. Emacs provides functions like `org-id-get-create` to generate IDs and `org-id-goto` to navigate to them.

Oxen walks through a directory of org-mode files, parses them to extract titles, tags, previews, and metadata, and generates HTML pages. It automatically builds tag pages that group related content together, creates a sitemap showing recent updates, and copies over any static files like CSS or images you might have. If your org files contain UUID properties (those handy `:ID:` properties Emacs can generate), Oxen builds a lookup system so that when converting org files to HTML, links to those IDs are resolved to links to the file-and-heading that defines them.

## Getting started

### Building from source

You'll need Go installed on your system. Oxen works with Go 1.25.5 and newer. To build the binary, run:

```
just build
```

This creates the `oxen` binary in the current directory. If you're on Linux and want to build specifically for that architecture, use `just build-linux`, or `just build-darwin` for macOS.

### Building your site

Once you have the binary, you can build a site from any directory containing org-mode files:

```
./oxen build /path/to/your/files
```

By default, Oxen outputs the generated HTML to a `public` directory. You can change this with the `--dest` flag:

```
./oxen build /path/to/your/files --dest output
```

If you want to force a complete rebuild (ignoring any cached files), use `--force`:

```
./oxen build /path/to/your/files --force
```

### Configuration

Configure Oxen using a `.oxen.json` file in your source directory or pass config via the `--config` flag:

```bash
./oxen build /path/to/your/files --config '{"site_name":"My Site","author":"Jane Doe"}'
```

See the [Configuration](#configuration) section below for all available options.

### Watching for changes

Oxen can watch your files for changes and automatically rebuild your site whenever you save something. This is perfect for live editing. Just add the `--watch` flag:

```
./oxen build /path/to/your/files --watch
```

The watch mode uses filesystem notifications to detect changes to org files, template files, and static assets. When it detects changes, it rebuilds only what's necessary and prints a summary of what it did.

### Live preview with server

When you're actively working on your site, you probably want to see the results in a browser as you edit. Oxen includes a built-in development server with live reload:

```
./oxen serve /path/to/your/files
```

This starts a web server on port 8080 (you can change this with `-p` or `--port`) and builds your site. When combined with `--watch`, the server will automatically reload any connected browsers whenever you make changes:

```
./oxen serve /path/to/your/files --watch -p 3000
```

The live reload works by injecting a small script into generated HTML pages that opens a connection to the server. When the rebuild completes, the server sends a signal to all connected browsers telling them to refresh.

## Looking up content by ID

Since Oxen already builds an in-memory index of all UUIDs and their locations, it gives you a command to look them up:

```
./oxen lookup-id /path/to/your/files 550e8400-e29b-41d4-a716-446655440000
```

## How it works

Oxen processes your org-mode files through a concurrent pipeline, generating a hypertext-aware static site while respecting your configuration and efficiently caching unchanged content.

The build process:

1. **Discovers and parses** all `.org` files in parallel, extracting titles, tags, UUIDs, and generating previews
2. **Indexes** all UUIDs for cross-referencing and builds tag-to-file mappings
3. **Generates HTML** with UUID link resolution, wrapping content in configurable templates
4. **Aggregates** supporting pages: tag indexes, sitemap with recent files, Atom feed, and static assets

The entire pipeline uses concurrent processing where possible and maintains thread-safe access to shared data structures. For detailed technical architecture, including the `BuildContext` system, concurrent processing model, and UUID resolution implementation, see [ARCHITECTURE.md](ARCHITECTURE.md).

## Templates

Oxen uses Go's `html/template` package, so you have access to the full template language for customization. Templates can be placed in a `templates` subdirectory of your source directory:

- `page-template.html` - Template for individual pages
- `tag-page-template.html` - Template for tag listing pages  
- `index-page-template.html` - Template for the main sitemap
- `base-template.html` - Base layout that other templates can extend

### Template Arguments

Each template receives different data structures:

**`page-template.html`** receives a `PageData` struct with these fields:
- `.Path` - File path (e.g., "posts/my-post.org")
- `.Title` - Title from file path
- `.Content` - Parsed HTML content
- `.ModTime` - File modification time
- `.Preview` - First 500 characters of content
- `.Tags` - Array of tag strings
- `.UUIDs` - Map of UUIDs in the file
- `.SiteName` - Site name from config
- `.BaseURL` - Base URL from config
- `.DefaultImage` - Default image path
- `.Author` - Author name
- `.LicenseName` - License name
- `.LicenseURL` - License URL

**`tag-page-template.html`** receives a `TagPageData` struct:
- `.Title` - Tag name
- `.Files` - Array of `FileInfo` structs with all files having this tag
- `.SiteName`, `.BaseURL`, `.DefaultImage`, `.Author`, `.LicenseName`, `.LicenseURL` (same as PageData)

**`index-page-template.html`** receives an `IndexPageData` struct:
- `.RecentFiles` - Array of 5 most recently modified files
- `.Tags` - Array of `TagInfo` structs with tag names and counts
- `.Content` - HTML from `sitemap-preamble.org` if it exists
- `.SiteName`, `.BaseURL`, `.DefaultImage`, `.Author`, `.LicenseName`, `.LicenseURL`

All templates have access to these helper functions:
- `pathNoExt` - Remove .org extension from paths
- `formatRFC3339` - Format time as RFC3339 string
- `sub` - Subtract two integers

## Configuration

You can configure Oxen using a `.oxen.json` file placed in the root of your source directory, or by passing JSON directly with the `--config` flag.

### Configuration File Location

Place `.oxen.json` in the same directory you pass to `oxen build` or `oxen serve`. For example, if you run `./oxen build ./my-site`, Oxen will look for `./my-site/.oxen.json`.

### Configuration Properties

All properties are optional. If not specified, defaults are used or fields are left empty.

```json
{
  "site_name": "My Site Name",
  "base_url": "https://example.com",
  "author": "John Doe", 
  "default_image": "/images/default.png",
  "license_name": "MIT License",
  "license_url": "https://opensource.org/licenses/MIT"
}
```

**`site_name`** (string): Name of your site, used in page titles and headers.

**`base_url`** (string): Base URL for your site (e.g., "https://mysite.com"). Used for absolute URLs in feeds and OpenGraph tags.

**`author`** (string): Author name, displayed in footers and Atom feeds.

**`default_image`** (string): Path to default OpenGraph image, relative to `base_url`. If `base_url` is "https://example.com" and `default_image` is "/img/og.png", the full URL becomes "https://example.com/img/og.png".

**`license_name`** (string): License name displayed in the footer (e.g., "MIT License", "GPL-3.0", "CC BY-SA 4.0"). If not specified, shows "all rights reserved".

**`license_url`** (string): URL to license text. If specified with `license_name`, creates a link in the footer.

### Command-Line Configuration

Pass JSON directly to override or supplement `.oxen.json`:

```bash
./oxen build ./my-site --config '{"site_name":"My Site","author":"Jane Doe"}'
```

The `--config` flag takes precedence over `.oxen.json`.

## Project structure

The code is organized into a few packages. The `generator` package handles all the build logic, split across multiple files: `phase1.go` does file discovery and parsing, `phase2.go` generates HTML pages, `phase3.go` builds tag pages and handles static files, and `types.go` contains the data structures used throughout. The `config` package handles loading configuration from `.oxen.json` or command-line flags. The `server` package contains the HTTP server with live reload support using Server-Sent Events.

Oxen uses a few external dependencies: `go-org` for parsing org-mode, `fsnotify` for watching files, and `cobra` for the command-line interface.

## License

Oxen is open source software. See the LICENSE file for details.
