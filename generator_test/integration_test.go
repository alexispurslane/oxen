package generator_test

import (
	"encoding/xml"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"oxen/generator"
)

func TestE2E_Phases1And2_IDLinkResolution(t *testing.T) {
	slog.Debug("FOOOOOOOOOOOOOOOOOO")
	tmpDir := createTempDir(t, "e2e-phases12-")
	defer os.RemoveAll(tmpDir)

	destDir := filepath.Join(tmpDir, "public")
	os.MkdirAll(destDir, 0755)

	createTestFile(t, tmpDir, "index.org", `#+title: Home Page
* Home :index:home:

Welcome to the site.

Check out [[id:550e8400-e29b-41d4-a716-446655440001][Document One]] or [[id:550e8400-e29b-41d4-a716-446655440002][Document Two]].
`)

	createTestFile(t, tmpDir, "doc1.org", `#+title: Document One
* Doc 1 :docs:
:PROPERTIES:
:ID:       550e8400-e29b-41d4-a716-446655440001
:END:

This is document one.

See also [[id:550e8400-e29b-41d4-a716-446655440002][Document Two]] and [[id:00000000-0000-0000-0000-000000000000][Home]].
`)

	subdir := filepath.Join(tmpDir, "subdir")
	os.MkdirAll(subdir, 0755)
	createTestFile(t, subdir, "doc2.org", `#+title: Document Two
* Doc 2 :docs:subdirectory:
:PROPERTIES:
:ID:       550e8400-e29b-41d4-a716-446655440002
:END:

This is document two in a subdirectory.

Link back to [[id:550e8400-e29b-41d4-a716-446655440001][Document One]] or to [[id:00000000-0000-0000-0000-000000000000][Home]].
`)

	createTestFile(t, tmpDir, "home.org", `#+title: Home
* Home :home:
:PROPERTIES:
:ID: 00000000-0000-0000-0000-000000000000
:END:


This is the actual home page.
`)

	ctx := &generator.BuildContext{
		Root:         tmpDir,
		DestDir:      destDir,
		ForceRebuild: true,
		TmplModTime:  time.Now(),
		SiteName:     "Test Site",
	}

	procFiles, result1 := generator.FindAndProcessOrgFiles(nil, *ctx)

	if result1.TotalFilesScanned != 4 {
		t.Errorf("Expected 4 files scanned, got %d", result1.TotalFilesScanned)
	}
	if result1.FilesWithUUIDs != 3 {
		t.Errorf("Expected 3 files with UUIDs, got %d", result1.FilesWithUUIDs)
	}

	uuid1, _ := procFiles.UuidMap.Load(generator.UUID("550e8400-e29b-41d4-a716-446655440001"))
	loc1 := uuid1.(generator.HeaderLocation)
	if loc1.FilePath != "doc1.org" {
		t.Errorf("Expected doc1.org for 550e8400-e29b-41d4-a716-446655440001, got %v", uuid1)
	}

	uuid2, _ := procFiles.UuidMap.Load(generator.UUID("550e8400-e29b-41d4-a716-446655440002"))
	loc2 := uuid2.(generator.HeaderLocation)
	if loc2.FilePath != "subdir/doc2.org" {
		t.Errorf("Expected subdir/doc2.org for 550e8400-e29b-41d4-a716-446655440002, got %v", uuid2)
	}

	pageTmpl, tagTmpl, indexTmpl, _, _, err := generator.SetupTemplates(tmpDir)
	if err != nil {
		t.Fatalf("SetupTemplates failed: %v", err)
	}
	_ = tagTmpl
	_ = indexTmpl

	result2 := generator.GenerateHtmlPages(procFiles, *ctx, pageTmpl)

	if result2.FilesGenerated != 4 {
		t.Errorf("Expected 4 files generated, got %d", result2.FilesGenerated)
	}

	verifyHTMLFile(t, destDir, "index.html", []string{
		`<a href="doc1.html#headline-1">`,
		`>Document One<`,
		`<a href="subdir/doc2.html#headline-1">`,
		`>Document Two<`,
	})

	verifyHTMLFile(t, destDir, "doc1.html", []string{
		`<a href="subdir/doc2.html#headline-1">`,
		`>Document Two<`,
		`<a href="home.html#headline-1">`,
		`>Home<`,
	})

	verifyHTMLFile(t, destDir, "subdir/doc2.html", []string{
		`<a href="../doc1.html#headline-1">`,
		`>Document One<`,
		`<a href="../home.html#headline-1">`,
		`>Home<`,
	})

	verifyHTMLFile(t, destDir, "home.html", []string{
		`<h1>Home</h1>`,
	})

	verifyNoBrokenIDLinks(t, destDir)
}

func TestE2E_ComplexDirectoryStructure(t *testing.T) {
	tmpDir := createTempDir(t, "e2e-complex-")
	defer os.RemoveAll(tmpDir)

	destDir := filepath.Join(tmpDir, "public")
	os.MkdirAll(destDir, 0755)

	createTestFile(t, tmpDir, "root.org", `#+title: Root
* Root :root:
:PROPERTIES:
:ID: 550e8400-e29b-41d4-a716-446655440010
:END:


Link to [[id:550e8400-e29b-41d4-a716-446655440011][Level 1A]] and [[id:550e8400-e29b-41d4-a716-446655440030][Deep Level 1B]].
`)

	dir1 := filepath.Join(tmpDir, "level1a")
	os.MkdirAll(dir1, 0755)
	createTestFile(t, dir1, "file1.org", `#+title: Level 1A
* Level 1A :level1:
:PROPERTIES:
:ID:       550e8400-e29b-41d4-a716-446655440011
:END:


Link to [[id:550e8400-e29b-41d4-a716-446655440010][Root]] and [[id:550e8400-e29b-41d4-a716-446655440020][Level 1B]].
`)

	dir2 := filepath.Join(tmpDir, "level1b")
	os.MkdirAll(dir2, 0755)
	createTestFile(t, dir2, "file2.org", `#+title: Level 1B
* Level 1B :level1:
:PROPERTIES:
:ID:       550e8400-e29b-41d4-a716-446655440020
:END:


Link to [[id:550e8400-e29b-41d4-a716-446655440010][Root]] and [[id:550e8400-e29b-41d4-a716-446655440011][Level 1A]].
`)

	deep := filepath.Join(dir2, "deep")
	os.MkdirAll(deep, 0755)
	createTestFile(t, deep, "nested.org", `#+title: Deep Nested

* Deep :deep:
:PROPERTIES:
:ID:       550e8400-e29b-41d4-a716-446655440030
:END:

Link to [[id:550e8400-e29b-41d4-a716-446655440010][Root]] and [[id:550e8400-e29b-41d4-a716-446655440020][Parent]].
`)

	ctx := &generator.BuildContext{
		Root:         tmpDir,
		DestDir:      destDir,
		ForceRebuild: true,
		TmplModTime:  time.Now(),
		SiteName:     "Complex Site",
	}

	procFiles, result1 := generator.FindAndProcessOrgFiles(nil, *ctx)
	if result1.TotalFilesScanned != 4 {
		t.Errorf("Expected 4 files scanned, got %d", result1.TotalFilesScanned)
	}

	pageTmpl, _, _, _, _, err := generator.SetupTemplates(tmpDir)
	if err != nil {
		t.Fatalf("SetupTemplates failed: %v", err)
	}

	result2 := generator.GenerateHtmlPages(procFiles, *ctx, pageTmpl)
	if result2.FilesGenerated != 4 {
		t.Errorf("Expected 4 files generated, got %d", result2.FilesGenerated)
	}

	destRoot := filepath.Join(destDir, "root.html")
	content, err := os.ReadFile(destRoot)
	if err != nil {
		t.Fatalf("Failed to read root.html: %v", err)
	}
	rootHTML := string(content)

	checkLinks(t, rootHTML, []linkCheck{
		{href: `href="level1a/file1.html#headline-1"`, text: "Level 1A"},
		{href: `href="level1b/deep/nested.html#headline-1"`, text: "Deep Level 1B"},
		{refuses: `id:`, desc: "No unresolved ID links"},
	})

	verifyHTMLFile(t, destDir, "level1a/file1.html", []string{
		`<a href="../root.html#headline-1">`,
		`>Root<`,
		`<a href="../level1b/file2.html#headline-1">`,
		`>Level 1B<`,
	})

	verifyHTMLFile(t, destDir, "level1b/deep/nested.html", []string{
		`<a href="../../root.html#headline-1">`,
		`>Root<`,
		`<a href="../file2.html#headline-1">`,
		`>Parent<`,
	})

	verifyNoBrokenIDLinks(t, destDir)
}

func TestE2E_UUIDLinkVariations(t *testing.T) {
	tmpDir := createTempDir(t, "e2e-variations-")
	defer os.RemoveAll(tmpDir)

	destDir := filepath.Join(tmpDir, "public")
	os.MkdirAll(destDir, 0755)

	createTestFile(t, tmpDir, "source.org", `#+title: Link Variations
* Variations :testing:

Different link styles:
- [[id:550e8400-e29b-41d4-a716-446655440101][Simple link]]
- Text before [[id:550e8400-e29b-41d4-a716-446655440102][inline link]] text after
- Multiple [[id:550e8400-e29b-41d4-a716-446655440103][first]] and [[id:550e8400-e29b-41d4-a716-446655440104][second]] links
- [[id:550e8400-e29b-41d4-a716-446655440105]]
`)

	createTestFile(t, tmpDir, "target1.org", `#+title: Target 1

* Target 1 :targets:
:PROPERTIES:
:ID:       550e8400-e29b-41d4-a716-446655440101
:END:

`)

	createTestFile(t, tmpDir, "target2.org", `#+title: Target 2
* Target 2 :targets:
:PROPERTIES:
:ID:       550e8400-e29b-41d4-a716-446655440102
:END:

`)

	createTestFile(t, tmpDir, "target3.org", `#+title: Target 3
* Target 3 :targets:
:PROPERTIES:
:ID:       550e8400-e29b-41d4-a716-446655440103
:END:

`)

	createTestFile(t, tmpDir, "target4.org", `#+title: Target 4
* Target 4 :targets:
:PROPERTIES:
:ID:       550e8400-e29b-41d4-a716-446655440104
:ID:       550e8400-e29b-41d4-a716-446655440105
:END:

`)

	ctx := &generator.BuildContext{
		Root:         tmpDir,
		DestDir:      destDir,
		ForceRebuild: true,
		TmplModTime:  time.Now(),
		SiteName:     "Variation Test",
	}

	procFiles, result1 := generator.FindAndProcessOrgFiles(nil, *ctx)
	if result1.FilesWithUUIDs != 4 {
		t.Errorf("Expected 4 files with UUIDs, got %d", result1.FilesWithUUIDs)
	}

	pageTmpl, _, _, _, _, err := generator.SetupTemplates(tmpDir)
	if err != nil {
		t.Fatalf("SetupTemplates failed: %v", err)
	}

	result2 := generator.GenerateHtmlPages(procFiles, *ctx, pageTmpl)
	if result2.FilesGenerated != 5 {
		t.Errorf("Expected 5 files generated, got %d", result2.FilesGenerated)
	}

	content := readHTMLFile(t, destDir, "source.html")

	checkLinks(t, content, []linkCheck{
		{href: `href="target1.html#headline-1"`, text: "Simple link"},
		{href: `href="target2.html#headline-1"`, text: "inline link"},
		{href: `href="target3.html#headline-1"`, text: "first"},
		{href: `href="target4.html#headline-1"`, text: "second"},
		{href: `href="target4.html#headline-1"`}, // No description link - org-mode uses the ID itself
		{refuses: `id:`, desc: "No unresolved ID links"},
		{refuses: `\]\[`, desc: "No org-mode link syntax remaining"},
	})

	verifyNoBrokenIDLinks(t, destDir)
}

func createTempDir(t *testing.T, prefix string) string {
	tmpDir, err := os.MkdirTemp("", prefix)
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	return tmpDir
}

func createTestFile(t *testing.T, dir, filename, content string) {
	err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file %s: %v", filename, err)
	}
}

func createTestFileWithModTime(t *testing.T, dir, filename, content string, modTime time.Time) {
	err := generator.CreateTestFileWithModTime(dir, filename, []byte(content), modTime)
	if err != nil {
		t.Fatalf("Failed to create test file %s: %v", filename, err)
	}
}

func readHTMLFile(t *testing.T, destDir, filename string) string {
	path := filepath.Join(destDir, filename)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read %s: %v", filename, err)
	}
	return string(content)
}

func verifyHTMLFile(t *testing.T, destDir, filename string, expectedStrings []string) {
	t.Helper()
	content := readHTMLFile(t, destDir, filename)

	for _, expected := range expectedStrings {
		if !strings.Contains(content, expected) {
			t.Errorf("File %s: expected to find %q", filename, expected)
			t.Logf("Content: %s", content[:min(500, len(content))])
		}
	}
}

type linkCheck struct {
	href    string
	text    string
	refuses string
	desc    string
}

func checkLinks(t *testing.T, content string, checks []linkCheck) {
	t.Helper()
	for _, check := range checks {
		if check.href != "" {
			if !strings.Contains(content, check.href) {
				t.Errorf("Expected link href %q not found", check.href)
			}
			if check.text != "" {
				if !strings.Contains(content, check.text) {
					t.Errorf("Expected link text %q not found", check.text)
				}
			}
		}
		if check.refuses != "" {
			if strings.Contains(content, check.refuses) {
				t.Errorf("Found unwanted content %q (%s)", check.refuses, check.desc)
			}
		}
	}
}

func verifyNoBrokenIDLinks(t *testing.T, destDir string) {
	err := filepath.Walk(destDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if strings.HasSuffix(path, ".html") {
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if strings.Contains(string(content), "id:") && strings.Contains(string(content), `href="id:`) {
				// Check if this is an actual unresolved ID link
				lines := strings.Split(string(content), "\n")
				for _, line := range lines {
					if strings.Contains(line, `href="id:`) || strings.Contains(line, `[[id:`) {
						t.Errorf("Found unresolved ID link in %s: %s", path, strings.TrimSpace(line))
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Failed to walk destDir: %v", err)
	}
}

type AtomFeed struct {
	XMLName xml.Name    `xml:"http://www.w3.org/2005/Atom feed"`
	Title   string      `xml:"title"`
	Links   []AtomLink  `xml:"link"`
	ID      string      `xml:"id"`
	Updated string      `xml:"updated"`
	Entries []AtomEntry `xml:"entry"`
}

type AtomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr,omitempty"`
}

type AtomEntry struct {
	Title      string         `xml:"title"`
	Links      []AtomLink     `xml:"link"`
	ID         string         `xml:"id"`
	Updated    string         `xml:"updated"`
	Summary    string         `xml:"summary"`
	Categories []AtomCategory `xml:"category"`
}

type AtomCategory struct {
	Term string `xml:"term,attr"`
}

func TestE2E_AtomFeed(t *testing.T) {
	tmpDir := createTempDir(t, "e2e-atom-")
	defer os.RemoveAll(tmpDir)

	destDir := filepath.Join(tmpDir, "public")
	os.MkdirAll(destDir, 0755)

	now := time.Now()
	// Create post1.org first (older timestamp)
	createTestFileWithModTime(t, tmpDir, "post1.org", `#+title: First Post
* First Post :blog:tech:
:PROPERTIES:
:ID:       550e8400-e29b-41d4-a716-446655440001
:END:

This is a blog post about Go programming.

It has multiple paragraphs for testing previews.`, now.Add(-1*time.Hour))

	// Create post2.org second (newer timestamp - should appear first in feed)
	createTestFileWithModTime(t, tmpDir, "post2.org", `#+title: Second Post  
* Second Post :blog:
:PROPERTIES:
:ID:       550e8400-e29b-41d4-a716-446655440002
:END:

This post covers testing and validation.

Using multiple paragraphs ensures proper preview extraction.`, now)

	ctx := &generator.BuildContext{
		Root:         tmpDir,
		DestDir:      destDir,
		ForceRebuild: true,
		TmplModTime:  time.Now(),
		SiteName:     "Test Blog",
	}

	procFiles, _ := generator.FindAndProcessOrgFiles(nil, *ctx)

	_, _, _, atomTmpl, _, err := generator.SetupTemplates(tmpDir)
	if err != nil {
		t.Fatalf("SetupTemplates failed: %v", err)
	}

	result := generator.GenerateAtomFeed(procFiles, *ctx, atomTmpl)
	if result.Errors != 0 {
		t.Errorf("GenerateAtomFeed() errors = %v, want 0", result.Errors)
	}

	feedPath := filepath.Join(destDir, "feed.xml")
	data, err := os.ReadFile(feedPath)
	if err != nil {
		t.Fatalf("Failed to read feed.xml: %v", err)
	}

	var feed AtomFeed
	if err := xml.Unmarshal(data, &feed); err != nil {
		t.Fatalf("Failed to parse feed.xml: %v", err)
	}

	if feed.Title != "Test Blog" {
		t.Errorf("Feed title = %q, want %q", feed.Title, "Test Blog")
	}

	if len(feed.Links) < 2 {
		t.Errorf("Feed should have at least 2 links, got %d", len(feed.Links))
	} else {
		hasSelf := false
		hasAlternate := false
		for _, link := range feed.Links {
			if link.Rel == "self" {
				hasSelf = true
				if !strings.Contains(link.Href, "/feed.xml") {
					t.Errorf("Self link href = %q, should contain /feed.xml", link.Href)
				}
			} else if link.Rel == "" {
				hasAlternate = true
				if !strings.HasSuffix(link.Href, "/") {
					t.Errorf("Alternate link href = %q, should end with /", link.Href)
				}
			}
		}
		if !hasSelf {
			t.Error("Feed missing self link")
		}
		if !hasAlternate {
			t.Error("Feed missing alternate link")
		}
	}

	if !strings.HasSuffix(feed.ID, "/") {
		t.Errorf("Feed ID = %q, should end with /", feed.ID)
	}

	if feed.Updated == "" {
		t.Error("Feed missing updated timestamp")
	}

	if len(feed.Entries) != 2 {
		t.Fatalf("Feed should have 2 entries, got %d", len(feed.Entries))
	}

	// Expected order (most recent first):
	// Entry 0: post2.org (Second Post, newer timestamp, blog tag)
	// Entry 1: post1.org (First Post, older timestamp, blog:tech tags)

	entry1 := feed.Entries[0]
	if entry1.Title != "Second Post" {
		t.Errorf("First entry title = %q, want %q", entry1.Title, "Second Post")
	}

	if len(entry1.Links) != 1 {
		t.Errorf("First entry should have 1 link, got %d", len(entry1.Links))
	} else if !strings.Contains(entry1.Links[0].Href, ".html") {
		t.Errorf("First entry link href = %q, should contain .html", entry1.Links[0].Href)
	}

	if entry1.ID == "" {
		t.Error("First entry missing ID")
	}

	if entry1.Updated == "" {
		t.Error("First entry missing updated timestamp")
	}

	if !strings.Contains(entry1.Summary, "testing and validation") {
		t.Errorf("First entry summary = %q, should contain 'testing and validation'", entry1.Summary)
	}

	expectedCategories1 := map[string]bool{"blog": false}
	for _, cat := range entry1.Categories {
		if _, exists := expectedCategories1[cat.Term]; exists {
			expectedCategories1[cat.Term] = true
		}
	}
	for cat, found := range expectedCategories1 {
		if !found {
			t.Errorf("First entry missing category %q", cat)
		}
	}

	entry2 := feed.Entries[1]
	if entry2.Title != "First Post" {
		t.Errorf("Second entry title = %q, want %q", entry2.Title, "First Post")
	}

	if !strings.Contains(entry2.Summary, "Go programming") {
		t.Errorf("Second entry summary = %q, should contain 'Go programming'", entry2.Summary)
	}

	expectedCategories2 := map[string]bool{"blog": false, "tech": false}
	for _, cat := range entry2.Categories {
		if _, exists := expectedCategories2[cat.Term]; exists {
			expectedCategories2[cat.Term] = true
		}
	}
	for cat, found := range expectedCategories2 {
		if !found {
			t.Errorf("Second entry missing category %q", cat)
		}
	}
}
