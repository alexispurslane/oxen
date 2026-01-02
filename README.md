# Oxen

*A simple, fast, standalone binary org-mode static site generator, built for hypertext enthusiasts and designed to last*

Think of it as a much simpler alternative to Hugo or Jekyll, or even Zola — designed specifically for those who live and breathe Emacs org-mode, but find `org-publish` too slow, or not portable enough (for instance, running headless in a CI system or in a systemd unit on your immutable core server).

What makes Oxen different: 

1. [Simple well known templating system](https://pkg.go.dev/html/template)
2. **Standalone Binary**: Compiles to a single, small, completely standalone binary (not even a dependency on libc!) which is trivially cross-compiled, making setup and installation a cinch, even on devices where you don't have the ability to install packages or compile things directly.
3. **Built to Last**: Built with completely vendored dependencies and no external build dependencies (e.g. C libraries): Oxen should be around for the long haul, because if you're using it — as I am — as your main way of expressing yourself to the world, then you need to be able to rely on it; vendored dependencies mean that even if I stop maintaining it, it will continue to work, since libraries and runtimes won't shift out from under it.
4. **Permissive License**: Licensed under the Unlicense, you can be confident that there are no restrictions on how you use Oxen for your own personal websites and content, as it should be. It's truly yours to do whatever you want with.

5. **Support for `org-mode`'s global ID system**: Other static site generators might be able to parse org syntax, but they have no concept of the `:ID:` properties that org-mode uses for filesystem-location invariant cross-referencing. When you build a site with Oxen, those UUIDs get resolved — the lookup-id command can tell you exactly which file contains a given ID, making it possible to maintain hypertext networks that survive the movement of sections between files, the renaming of sections, or file moves and renames.

6. **Near-Instant Feedback**: a full from-scratch rebuild of a 140-file, 700-UUID, 700,000-word project takes about 500 milliseconds. When you combine that performance with watch mode and the built-in dev server with live reload, you get an instant feedback loop for writing and editing. Save a file, and within half a second at most (it also has incremental builds) your browser refreshes with the changes.

7. An aspiration to be **Finished Software**: Oxen, at some point soon, should be done with a capital-D. There are only so many features you need in an SSG for org-mode second brains, really!

## What it does

Oxen walks through a directory of org-mode files, parses them to extract titles, tags, previews, and metadata, and generates HTML pages. It automatically builds tag pages that group related content together, creates a sitemap showing recent updates, and copies over any static files like CSS or images you might have. If your org files contain UUID properties (those handy `:ID:` properties Emacs can generate), Oxen builds a lookup system so that when converting org files to HTML, links to those IDs are resolved to links to the files that define them.

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

You can also set a custom site name with `--site-name`:

```
./oxen build /path/to/your/files --site-name "My Awesome Site"
```

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

## How it works

Oxen's build process happens in several phases:

First, it walks through your source directory and finds all the `.org` files. Then it processes each file in parallel to extract metadata like the title, tags, modification time, and any UUIDs found in properties drawers. It also generates a short preview by stripping out org-mode syntax like properties blocks and keywords and then taking the first 500 characters. During this process, Oxen generates an in-memory index of all of the UUID locations, to resolve them when it comes time to generate working HTML links.

Next, Oxen loads the HTML templates used to render pages. It looks for template files in your source directory's `templates` subdirectory, or falls back to embedded defaults. If your templates are newer than the last build, Oxen knows to regenerate pages.

The core of the build is converting each org-mode file to HTML. Oxen uses the excellent `go-org` library to parse org syntax and render it as semantic HTML. It then wraps this content in a template that includes your site navigation, footer, and styling. Each page gets the full treatment — titles, tags, and all the content you'd expect. This is also when org `id:` links are resolved, prior to HTML generation.

Once all the individual pages are generated, Oxen creates tag pages. Each unique tag gets its own page listing all the files tagged with it, sorted by modification time. This makes it easy to discover related content across your site.

Finally, Oxen generates a main index page that shows recent updates and a list of all tags, plus copies over any static files from your source directory that aren't org-mode files. Things like CSS, JavaScript, images, and fonts all get copied over directly.

## Looking up content by ID

Org-mode has built-in support for UUIDs as global identifiers for entries through the built in `org-id.el` package. These are stored as `:ID:` properties in your org files and serve as automatically generated filesystem-location and section-title invariant references to content. Instead of linking to files by their path in the filesystem hierarchy, or sections by title name, you link to content by these persistent IDs. If you move or rename a file or a section, the links still work because the ID travels with the entry.

This approach is particularly useful if you practice Zettelkasten, take a classic hypertext perspective à la Ted Nelson, or simply prefer an information architecture that doesn't rely on rigid file hierarchies. Emacs provides functions like `org-id-get-create` to generate IDs and `org-id-goto` to navigate to them.

Most static site generators — even those that can parse org-mode — completely ignore this feature. They treat IDs as just another property and provide no way to resolve them. Oxen is different: it builds an in-memory index of all IDs and their locations, then gives you a command to look them up:

```
./oxen lookup-id /path/to/your/files 550e8400-e29b-41d4-a716-446655440000
```

This tells you exactly which file contains the entry with that ID, making it possible to build tools that work with your hypertext network rather than against it.

```
./oxen lookup-id /path/to/your/files 550e8400-e29b-41d4-a716-446655440000
```

## Templates

Oxen uses Go's `html/template` package, so you have access to the full template language for customization. Templates can be placed in a `templates` subdirectory of your source directory:

- `page-template.html` - Template for individual pages
- `tag-page-template.html` - Template for tag listing pages
- `index-page-template.html` - Template for the main sitemap
- `base-template.html` - Base layout that other templates can extend

Each template receives data about the file being rendered, including the parsed HTML content, title, tags, modification time, and other metadata. You can customize the HTML structure, add analytics scripts, or completely change the look and feel by editing these templates.

## Project structure

The code is organized into a few packages. The `generator` package handles all the build logic, split across multiple files: `phase1.go` does file discovery and parsing, `phase2.go` generates HTML pages, `phase3.go` builds tag pages and handles static files, and `types.go` contains the data structures used throughout. The `server` package contains the HTTP server with live reload support using Server-Sent Events.

Oxen uses a few external dependencies: `go-org` for parsing org-mode, `fsnotify` for watching files, and `cobra` for the command-line interface.

## License

Oxen is open source software. See the LICENSE file for details.
