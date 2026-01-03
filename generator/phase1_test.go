package generator

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestExtractTitle(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected string
	}{
		{
			name: "title_from_directive",
			content: `#+title: My Test Document
* Headline here
Content here`,
			expected: "My Test Document",
		},
		{
			name: "title_from_headline",
			content: `* My Headline Title :tag1:tag2:
Content here`,
			expected: "My Headline Title",
		},
		{
			name: "title_directive_takes_precedence",
			content: `#+title: Directive Title
* Headline Title :tag:
Content`,
			expected: "Directive Title",
		},
		{
			name: "no_title",
			content: `Just some content
without a title`,
			expected: "",
		},
		{
			name: "empty_title_directive",
			content: `#+title: 
* Headline Title
Content`,
			expected: "Headline Title",
		},
		{
			name: "title_with_unicode_and_punctuation",
			content: `#+title: 你好! Test: Document (v2.0)
* Headline`,
			expected: "你好! Test: Document (v2.0)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractTitle([]byte(tt.content))
			if result != tt.expected {
				t.Errorf("extractTitle() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestExtractTags(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected []string
	}{
		{
			name: "single_tag",
			content: `* Headline :emacs:
Content`,
			expected: []string{"emacs"},
		},
		{
			name: "multiple_tags",
			content: `* Headline :emacs:org-mode:testing:
Content`,
			expected: []string{"emacs", "org-mode", "testing"},
		},
		{
			name: "tags_with_text_after",
			content: `* Headline :tag1:tag2: some more text
Content here`,
			expected: []string{"tag1", "tag2"},
		},
		{
			name: "no_tags",
			content: `* Headline without tags
Content`,
			expected: nil,
		},
		{
			name: "no_headline",
			content: `Just content
no headline`,
			expected: nil,
		},
		{
			name: "empty_tags",
			content: `* Headline ::
Content`,
			expected: nil,
		},
		{
			name: "only_first_headline_considered",
			content: `* First Headline :tag1:
Content
* Second Headline :tag2:
More content`,
			expected: []string{"tag1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractTags([]byte(tt.content))
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("extractTags() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestExtractPreview(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		maxLen   int
		expected string
	}{
		{
			name: "basic_preview",
			content: `* Title :tag:
This is the first paragraph of content.
And this is the second paragraph.`,
			maxLen:   100,
			expected: "This is the first paragraph of content. And this is the second paragraph.",
		},
		{
			name: "preview_with_drawers_removed",
			content: `* Title
:PROPERTIES:
:ID: test-id
:END:
Actual content here.`,
			maxLen:   50,
			expected: "Actual content here.",
		},
		{
			name: "preview_with_org_keywords_removed",
			content: `* Title
#+title: Test
#+author: Author
Content here.`,
			maxLen:   50,
			expected: "Content here.",
		},
		{
			name: "preview_with_links_simplified",
			content: `* Title
Here is a [[https://example.com][link description]] and a [[file:test.org][file link]].`,
			maxLen:   100,
			expected: "Here is a link description and a file link.",
		},
		{
			name: "preview_with_emphasis_removed",
			content: `* Title
This is *bold* and /italic/ and _underlined_ text.`,
			maxLen:   100,
			expected: "This is bold and italic and underlined text.",
		},
		{
			name: "preview_with_blocks_removed",
			content: `* Title
Content before block.
#+begin_src
Code here
#+end_src
Content after block.`,
			maxLen:   100,
			expected: "Content before block. Code here Content after block.",
		},
		{
			name: "preview_truncated",
			content: `* Title
This is a very long content that should be truncated when it exceeds the maximum length limit for preview generation.`,
			maxLen:   50,
			expected: "This is a very long content that should be truncat...",
		},
		{
			name:     "empty_content",
			content:  "",
			maxLen:   50,
			expected: "",
		},
		{
			name: "only_title_no_content",
			content: `* Title :tag:
`,
			maxLen:   50,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractPreview([]byte(tt.content), tt.maxLen)
			if result != tt.expected {
				t.Errorf("extractPreview() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestProcessFile(t *testing.T) {
	tmpDir := MustCreateTempDir(t, "test-process-")
	defer CleanupTempDir(tmpDir)

	testContent := `#+title: Test Document
* Test Headline :emacs:testing:

This is the first paragraph of the document.
It contains some content that will be used for the preview.

#+begin_src
Some code block
#+end_src

:ID: 550e8400-e29b-41d4-a716-446655440000
:ID: 123e4567-e89b-12d3-a456-426614174000
`

	orgFile := filepath.Join(tmpDir, "test.org")
	os.WriteFile(orgFile, []byte(testContent), 0644)

	procFiles := &ProcessedFiles{
		Files:   []FileInfo{},
		UuidMap: sync.Map{},
		TagMap:  sync.Map{},
	}

	result, err := processFile("test.org", tmpDir, procFiles)
	if err != nil {
		t.Fatalf("processFile() error = %v", err)
	}

	if result.Title != "Test Document" {
		t.Errorf("Title = %q, want %q", result.Title, "Test Document")
	}

	expectedTags := []string{"emacs", "testing"}
	if !reflect.DeepEqual(result.Tags, expectedTags) {
		t.Errorf("Tags = %v, want %v", result.Tags, expectedTags)
	}

	expectedUUIDs := []string{"550e8400-e29b-41d4-a716-446655440000", "123e4567-e89b-12d3-a456-426614174000"}
	if !reflect.DeepEqual(result.UUIDs, expectedUUIDs) {
		t.Errorf("UUIDs = %v, want %v", result.UUIDs, expectedUUIDs)
	}

	if len(result.Preview) == 0 {
		t.Error("Preview is empty")
	}
	if !strings.Contains(result.Preview, "first paragraph") {
		t.Error("Preview doesn't contain expected content")
	}

	for _, uuid := range expectedUUIDs {
		storedPath, ok := procFiles.UuidMap.Load("id:" + uuid)
		if !ok {
			t.Errorf("UUID %s not found in UuidMap", uuid)
		}
		if storedPath != "test.org" {
			t.Errorf("UuidMap[%s] = %v, want test.org", uuid, storedPath)
		}
	}

}

func TestCollectOrgFiles(t *testing.T) {
	tmpDir := MustCreateTempDir(t, "test-collect-")
	defer CleanupTempDir(tmpDir)

	CreateTestFileWithModTime(tmpDir, "test1.org", []byte("content"), time.Now())
	CreateTestFileWithModTime(tmpDir, "test2.org", []byte("content"), time.Now())
	os.MkdirAll(filepath.Join(tmpDir, "subdir"), 0755)
	CreateTestFileWithModTime(tmpDir, "subdir/nested.org", []byte("content"), time.Now())
	CreateTestFileWithModTime(tmpDir, "notorg.txt", []byte("content"), time.Now())

	result := collectOrgFiles(tmpDir)

	if len(result) != 3 {
		t.Errorf("collectOrgFiles() returned %d files, want 3", len(result))
	}

	paths := make(map[string]bool)
	for _, f := range result {
		paths[f.Path] = true
	}

	expectedPaths := []string{"test1.org", "test2.org", "subdir/nested.org"}
	for _, p := range expectedPaths {
		if !paths[p] {
			t.Errorf("Expected path %s not found in results", p)
		}
	}

	if paths["notorg.txt"] {
		t.Error("notorg.txt should not be included in results")
	}
}

func TestFindAndProcessOrgFiles(t *testing.T) {
	tmpDir := MustCreateTempDir(t, "test-phase1-")
	defer CleanupTempDir(tmpDir)

	CreateTestOrgFile(tmpDir, "doc1.org", `#+title: Document One
* First Doc :emacs:org:

This is the first document content.

:id: 550e8400-e29b-41d4-a716-446655440001
`)

	CreateTestOrgFile(tmpDir, "doc2.org", `* Second Document :emacs:

This is the second document content.
`)

	os.MkdirAll(filepath.Join(tmpDir, "subdir"), 0755)
	CreateTestOrgFile(tmpDir, "subdir/nested.org", `#+title: Nested Doc
* Nested :testing:

Nested content here.

:id: 550e8400-e29b-41d4-a716-446655440002
`)

	ctx := CreateTestBuildContext(tmpDir, "", "Test Site", false)
	procFiles, result := FindAndProcessOrgFiles(*ctx)

	if result.TotalFilesScanned != 3 {
		t.Errorf("TotalFilesScanned = %d, want 3", result.TotalFilesScanned)
	}

	if result.FilesWithUUIDs != 2 {
		t.Errorf("FilesWithUUIDs = %d, want 2", result.FilesWithUUIDs)
	}

	if len(procFiles.Files) != 3 {
		t.Errorf("Processed files count = %d, want 3", len(procFiles.Files))
	}

	expectedTitles := map[string]string{
		"doc1.org":          "Document One",
		"doc2.org":          "Second Document",
		"subdir/nested.org": "Nested Doc",
	}

	for _, fi := range procFiles.Files {
		if expected, ok := expectedTitles[fi.Path]; ok {
			if fi.Title != expected {
				t.Errorf("Title for %s = %q, want %q", fi.Path, fi.Title, expected)
			}
		}
	}

	emacsFiles, _ := procFiles.TagMap.Load("emacs")
	if emacsFiles == nil {
		t.Error("emacs tag not found in TagMap")
	} else if files := emacsFiles.([]FileInfo); len(files) != 2 {
		t.Errorf("emacs tag has %d files, want 2", len(files))
	} else {
		// Verify the specific files with emacs tag (order may vary)
		paths := make([]string, len(files))
		for i, f := range files {
			paths[i] = f.Path
		}
		expectedPaths := map[string]bool{"doc1.org": true, "doc2.org": true}
		for _, p := range paths {
			if !expectedPaths[p] {
				t.Errorf("emacs tag contains unexpected file: %s", p)
			}
		}
	}

	testingFiles, _ := procFiles.TagMap.Load("testing")
	if testingFiles == nil {
		t.Error("testing tag not found in TagMap")
	} else if files := testingFiles.([]FileInfo); len(files) != 1 {
		t.Errorf("testing tag has %d files, want 1", len(files))
	} else if files[0].Path != "subdir/nested.org" {
		t.Errorf("testing tag file = %s, want subdir/nested.org", files[0].Path)
	}

	// Verify UUID paths in UuidMap
	storedPath1, ok := procFiles.UuidMap.Load("id:550e8400-e29b-41d4-a716-446655440001")
	if !ok {
		t.Error("UUID from doc1.org not found in UuidMap")
	} else if storedPath1 != "doc1.org" {
		t.Errorf("UuidMap[doc1 UUID] = %v, want doc1.org", storedPath1)
	}

	storedPath2, ok := procFiles.UuidMap.Load("id:550e8400-e29b-41d4-a716-446655440002")
	if !ok {
		t.Error("UUID from doc2.org not found in UuidMap")
	} else if storedPath2 != "subdir/nested.org" {
		t.Errorf("UuidMap[subdir/nested.org UUID] = %v, want subdir/nested.org", storedPath2)
	}

}
