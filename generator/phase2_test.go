package generator

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReplaceUUIDLinks(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		keywords    []string
		paths       []string
		currentFile string
		expected    string
	}{
		{
			name:        "single_uuid_replacement",
			content:     `[[id:550e8400-e29b-41d4-a716-446655440000][link]]`,
			keywords:    []string{"id:550e8400-e29b-41d4-a716-446655440000"},
			paths:       []string{"/path/to/target"},
			currentFile: "/path/to/source",
			expected:    `[[file:target.html][link]]`,
		},
		{
			name:        "multiple_uuid_replacements",
			content:     `[[id:550e8400-e29b-41d4-a716-446655440000][link1]] and [[id:550e8400-e29b-41d4-a716-446655440001][link2]]`,
			keywords:    []string{"id:550e8400-e29b-41d4-a716-446655440000", "id:550e8400-e29b-41d4-a716-446655440001"},
			paths:       []string{"/path/to/target1", "/path/to/target2"},
			currentFile: "/path/to/source",
			expected:    `[[file:target1.html][link1]] and [[file:target2.html][link2]]`,
		},
		{
			name:        "uuid_not_found",
			content:     `[[id:unknown-uuid][broken link]]`,
			keywords:    []string{"id:550e8400-e29b-41d4-a716-446655440000"},
			paths:       []string{"/path/to/target"},
			currentFile: "/path/to/source",
			expected:    `[[id:unknown-uuid][broken link]]`, // Should leave unchanged
		},
		{
			name:        "no_uuid_links",
			content:     `This has no UUID links`,
			keywords:    []string{"id:550e8400-e29b-41d4-a716-446655440000"},
			paths:       []string{"/path/to/target"},
			currentFile: "/path/to/source",
			expected:    `This has no UUID links`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := replaceUUIDLinks([]byte(tt.content), tt.keywords, tt.paths, tt.currentFile)
			if string(result) != tt.expected {
				t.Errorf("replaceUUIDLinks() = %q, want %q", string(result), tt.expected)
			}
		})
	}
}

func TestReplaceUUIDLinks_EdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		keywords    []string
		paths       []string
		currentFile string
		expected    string
	}{
		{
			name:        "same_directory",
			content:     `[[id:test-uuid][link]]`,
			keywords:    []string{"id:test-uuid"},
			paths:       []string{"/same/dir/target"},
			currentFile: "/same/dir/source",
			expected:    `[[file:target.html][link]]`,
		},
		{
			name:        "subdirectory",
			content:     `[[id:test-uuid][link]]`,
			keywords:    []string{"id:test-uuid"},
			paths:       []string{"/root/sub/target"},
			currentFile: "/root/source",
			expected:    `[[file:sub/target.html][link]]`,
		},
		{
			name:        "parent_directory",
			content:     `[[id:test-uuid][link]]`,
			keywords:    []string{"id:test-uuid"},
			paths:       []string{"/root/target"},
			currentFile: "/root/sub/source",
			expected:    `[[file:../target.html][link]]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := replaceUUIDLinks([]byte(tt.content), tt.keywords, tt.paths, tt.currentFile)
			if string(result) != tt.expected {
				t.Errorf("replaceUUIDLinks() = %q, want %q", string(result), tt.expected)
			}
		})
	}
}

func TestSetupTemplates_Embedded(t *testing.T) {
	tmpDir := MustCreateTempDir(t, "test-templates-")
	defer CleanupTempDir(tmpDir)

	pageTmpl, tagTmpl, indexTmpl, modTime, err := SetupTemplates(tmpDir)
	if err != nil {
		t.Fatalf("SetupTemplates() error = %v", err)
	}

	if pageTmpl == nil || tagTmpl == nil || indexTmpl == nil {
		t.Error("SetupTemplates() returned nil template(s)")
	}

	if modTime.IsZero() {
		t.Error("SetupTemplates() returned zero modification time")
	}
}

func TestSetupTemplates_Custom(t *testing.T) {
	tmpDir := MustCreateTempDir(t, "test-templates-")
	defer CleanupTempDir(tmpDir)

	templatesDir := filepath.Join(tmpDir, "templates")
	os.MkdirAll(templatesDir, 0755)

	baseTmpl := `<html><body>{{template "content" .}}</body></html>`
	pageTmpl := `{{define "content"}}<h1>{{.Title}}</h1>{{end}}`
	tagTmpl := `{{define "content"}}<h1>Tag: {{.Tag}}</h1>{{end}}`
	indexTmpl := `{{define "content"}}<h1>Index</h1>{{end}}`

	os.WriteFile(filepath.Join(templatesDir, "base-template.html"), []byte(baseTmpl), 0644)
	os.WriteFile(filepath.Join(templatesDir, "page-template.html"), []byte(pageTmpl), 0644)
	os.WriteFile(filepath.Join(templatesDir, "tag-page-template.html"), []byte(tagTmpl), 0644)
	os.WriteFile(filepath.Join(templatesDir, "index-page-template.html"), []byte(indexTmpl), 0644)

	page, tag, index, modTime, err := SetupTemplates(tmpDir)
	if err != nil {
		t.Fatalf("SetupTemplates() error = %v", err)
	}

	if page == nil || tag == nil || index == nil {
		t.Error("SetupTemplates() returned nil template(s)")
	}

	if modTime.IsZero() {
		t.Error("SetupTemplates() returned zero modification time")
	}
}

func TestSetupTemplates_InvalidCustom(t *testing.T) {
	tmpDir := MustCreateTempDir(t, "test-templates-")
	defer CleanupTempDir(tmpDir)

	templatesDir := filepath.Join(tmpDir, "templates")
	os.MkdirAll(templatesDir, 0755)

	// Create invalid template
	os.WriteFile(filepath.Join(templatesDir, "base-template.html"), []byte("{{.Invalid"), 0644)

	_, _, _, _, err := SetupTemplates(tmpDir)
	if err == nil {
		t.Error("SetupTemplates() expected error for invalid template, got nil")
	}
}

func TestExecuteTemplates_Embedded(t *testing.T) {
	pageTmpl, tagTmpl, indexTmpl, _, err := SetupTemplates("/nonexistent") // Force embedded
	if err != nil {
		t.Fatalf("SetupTemplates() error = %v", err)
	}

	// Test page template execution
	t.Run("page_template", func(t *testing.T) {
		data := PageData{
			FileInfo: FileInfo{
				Title: "Test Page",
				Path:  "test.org",
			},
			Content:  "<p>Test content</p>",
			SiteName: "Test Site",
		}

		var buf bytes.Buffer
		if err := pageTmpl.ExecuteTemplate(&buf, "page-template.html", data); err != nil {
			t.Errorf("Page template execution failed: %v", err)
		}

		output := buf.String()
		if !strings.Contains(output, "Test Page") {
			t.Error("Page template doesn't contain title")
		}
		if !strings.Contains(output, "Test content") {
			t.Error("Page template doesn't contain content")
		}
	})

	// Test tag template execution
	t.Run("tag_template", func(t *testing.T) {
		data := TagPageData{
			Title:    "test-tag",
			Files:    []FileInfo{{Title: "File 1"}},
			SiteName: "Test Site",
		}

		var buf bytes.Buffer
		if err := tagTmpl.ExecuteTemplate(&buf, "tag-page-template.html", data); err != nil {
			t.Errorf("Tag template execution failed: %v", err)
		}

		output := buf.String()
		if !strings.Contains(output, "test-tag") {
			t.Error("Tag template doesn't contain tag name")
		}
		if !strings.Contains(output, "File 1") {
			t.Error("Tag template doesn't contain file list")
		}
	})

	// Test index template execution
	t.Run("index_template", func(t *testing.T) {
		data := IndexPageData{
			RecentFiles: []FileInfo{{Title: "Recent 1"}},
			Tags:        []TagInfo{{Name: "tag1", Count: 1}},
			Content:     "<p>Index content</p>",
			SiteName:    "Test Site",
		}

		var buf bytes.Buffer
		if err := indexTmpl.ExecuteTemplate(&buf, "index-page-template.html", data); err != nil {
			t.Errorf("Index template execution failed: %v", err)
		}

		output := buf.String()
		if !strings.Contains(output, "Recent 1") {
			t.Error("Index template doesn't contain recent files")
		}
		if !strings.Contains(output, "tag1") {
			t.Error("Index template doesn't contain tags")
		}
	})
}

func TestExecuteTemplates_Custom(t *testing.T) {
	tmpDir := MustCreateTempDir(t, "test-templates-")
	defer CleanupTempDir(tmpDir)

	templatesDir := filepath.Join(tmpDir, "templates")
	os.MkdirAll(templatesDir, 0755)

	// Custom templates with identifiable markers
	baseContents := `<!DOCTYPE html><html><body>{{block "content" .}}Default{{end}}</body></html>`
	pageContents := `{{define "content"}}<article><h1>CUSTOM: {{.Title}}</h1><div class="content">{{.Content}}</div><footer>{{.SiteName}}</footer></article>{{end}}{{template "base-template.html" .}}`
	tagContents := `{{define "content"}}<section><h1>CUSTOM TAG: {{.Title}}</h1><p>{{len .Files}} files</p><ul>{{range .Files}}<li>{{.Title}}</li>{{end}}</ul><footer>{{.SiteName}}</footer></section>{{end}}{{template "base-template.html" .}}`
	indexContents := `{{define "content"}}<main><h1>CUSTOM INDEX</h1><section class="recent">{{range .RecentFiles}}{{.Title}}{{end}}</section><section class="tags">{{range .Tags}}{{.Name}}:{{.Count}}{{end}}</section><div class="content">{{.Content}}</div><footer>{{.SiteName}}</footer></main>{{end}}{{template "base-template.html" .}}`

	os.WriteFile(filepath.Join(templatesDir, "base-template.html"), []byte(baseContents), 0644)
	os.WriteFile(filepath.Join(templatesDir, "page-template.html"), []byte(pageContents), 0644)
	os.WriteFile(filepath.Join(templatesDir, "tag-page-template.html"), []byte(tagContents), 0644)
	os.WriteFile(filepath.Join(templatesDir, "index-page-template.html"), []byte(indexContents), 0644)

	pageTmpl, tagTmpl, indexTmpl, _, err := SetupTemplates(tmpDir)
	if err != nil {
		t.Fatalf("SetupTemplates() error = %v", err)
	}

	// Verify custom templates are actually used by checking for custom markers
	t.Run("custom_page_template", func(t *testing.T) {
		data := PageData{
			FileInfo: FileInfo{
				Title: "Test",
				Path:  "test.org",
			},
			Content:  "Content",
			SiteName: "Site",
		}

		var buf bytes.Buffer
		if err := pageTmpl.ExecuteTemplate(&buf, "page-template.html", data); err != nil {
			t.Errorf("Custom page template execution failed: %v", err)
		}

		output := buf.String()
		if !strings.Contains(output, "CUSTOM:") {
			t.Error("Custom page template marker not found")
		}
		if !strings.Contains(output, "Test") {
			t.Error("Page title 'Test' not found in output")
		}
		if !strings.Contains(output, "Content") {
			t.Error("Page content 'Content' not found in output")
		}
		if !strings.Contains(output, "Site") {
			t.Error("Site name 'Site' not found in output")
		}
	})

	t.Run("custom_tag_template", func(t *testing.T) {
		data := TagPageData{
			Title: "test-tag",
			Files: []FileInfo{{
				Title: "Test File",
				Path:  "test.org",
			}},
			SiteName: "Site",
		}

		var buf bytes.Buffer
		if err := tagTmpl.ExecuteTemplate(&buf, "tag-page-template.html", data); err != nil {
			t.Errorf("Custom tag template execution failed: %v", err)
		}

		output := buf.String()
		if !strings.Contains(output, "CUSTOM TAG:") {
			t.Error("Custom tag template marker not found")
		}
		if !strings.Contains(output, "test-tag") {
			t.Error("Tag title 'test-tag' not found in output")
		}
		if !strings.Contains(output, "1 files") {
			t.Error("File count '1' not found in output")
		}
	})

	t.Run("custom_index_template", func(t *testing.T) {
		data := IndexPageData{
			RecentFiles: []FileInfo{{
				Title: "Recent File",
				Path:  "recent.org",
			}},
			Tags:     []TagInfo{{Name: "tag1", Count: 5}},
			Content:  "<p>Some content</p>",
			SiteName: "Test Site",
		}

		var buf bytes.Buffer
		if err := indexTmpl.ExecuteTemplate(&buf, "index-page-template.html", data); err != nil {
			t.Errorf("Custom index template execution failed: %v", err)
		}

		output := buf.String()
		if !strings.Contains(output, "CUSTOM INDEX") {
			t.Error("Custom index template marker not found")
		}
		if !strings.Contains(output, "Test Site") {
			t.Error("Site name 'Test Site' not found in output")
		}
	})
}
