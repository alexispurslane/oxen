package generator_test

import (
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

	uuid1, _ := procFiles.UuidMap.Load("id:550e8400-e29b-41d4-a716-446655440001")
	if uuid1 != "doc1.org" {
		t.Errorf("Expected doc1.org for 550e8400-e29b-41d4-a716-446655440001, got %v", uuid1)
	}

	uuid2, _ := procFiles.UuidMap.Load("id:550e8400-e29b-41d4-a716-446655440002")
	if uuid2 != "subdir/doc2.org" {
		t.Errorf("Expected subdir/doc2.org for 550e8400-e29b-41d4-a716-446655440002, got %v", uuid2)
	}

	pageTmpl, tagTmpl, indexTmpl, _, err := generator.SetupTemplates(tmpDir)
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
		`<a href="doc1.html">`,
		`>Document One<`,
		`<a href="subdir/doc2.html">`,
		`>Document Two<`,
	})

	verifyHTMLFile(t, destDir, "doc1.html", []string{
		`<a href="subdir/doc2.html">`,
		`>Document Two<`,
		`<a href="home.html">`,
		`>Home<`,
	})

	verifyHTMLFile(t, destDir, "subdir/doc2.html", []string{
		`<a href="../doc1.html">`,
		`>Document One<`,
		`<a href="../home.html">`,
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

	pageTmpl, _, _, _, err := generator.SetupTemplates(tmpDir)
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
		{href: `href="level1a/file1.html"`, text: "Level 1A"},
		{href: `href="level1b/deep/nested.html"`, text: "Deep Level 1B"},
		{refuses: `id:`, desc: "No unresolved ID links"},
	})

	verifyHTMLFile(t, destDir, "level1a/file1.html", []string{
		`<a href="../root.html">`,
		`>Root<`,
		`<a href="../level1b/file2.html">`,
		`>Level 1B<`,
	})

	verifyHTMLFile(t, destDir, "level1b/deep/nested.html", []string{
		`<a href="../../root.html">`,
		`>Root<`,
		`<a href="../file2.html">`,
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

	pageTmpl, _, _, _, err := generator.SetupTemplates(tmpDir)
	if err != nil {
		t.Fatalf("SetupTemplates failed: %v", err)
	}

	result2 := generator.GenerateHtmlPages(procFiles, *ctx, pageTmpl)
	if result2.FilesGenerated != 5 {
		t.Errorf("Expected 5 files generated, got %d", result2.FilesGenerated)
	}

	content := readHTMLFile(t, destDir, "source.html")

	checkLinks(t, content, []linkCheck{
		{href: `href="target1.html"`, text: "Simple link"},
		{href: `href="target2.html"`, text: "inline link"},
		{href: `href="target3.html"`, text: "first"},
		{href: `href="target4.html"`, text: "second"},
		{href: `href="target4.html"`}, // No description link - org-mode uses the ID itself
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
