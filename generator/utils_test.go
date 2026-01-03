package generator

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func MustCreateTempDir(t *testing.T, prefix string) string {
	dir, err := os.MkdirTemp("", prefix)
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	return dir
}

func TestExtractUUIDs(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected []string
	}{
		{
			name:     "single_uuid",
			content:  ":PROPERTIES:\n:ID:       550e8400-e29b-41d4-a716-446655440000\n:END:",
			expected: []string{"550e8400-e29b-41d4-a716-446655440000"},
		},
		{
			name:     "multiple_uuids_lowercase_and_uppercase",
			content:  ":PROPERTIES:\n:ID:       550e8400-e29b-41d4-a716-446655440000\n:END:\n\n:PROPERTIES:\n:id:       550e8400-e29b-41d4-a716-446655440001\n:END:",
			expected: []string{"550e8400-e29b-41d4-a716-446655440000", "550e8400-e29b-41d4-a716-446655440001"},
		},
		{
			name:     "no_uuid",
			content:  "This content has no UUIDs",
			expected: nil,
		},
		{
			name:     "whitespace_handling",
			content:  ":PROPERTIES:\n:ID:         550e8400-e29b-41d4-a716-446655440000\n:END:",
			expected: []string{"550e8400-e29b-41d4-a716-446655440000"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractUUIDs([]byte(tt.content))
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("extractUUIDs() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestIsValidUUID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"valid_uuid_lowercase", "550e8400-e29b-41d4-a716-446655440000", true},
		{"valid_uuid_uppercase", "550E8400-E29B-41D4-A716-446655440000", true},
		{"valid_uuid_mixed", "550e8400-E29B-41d4-a716-446655440000", true},
		{"short_string", "550e8400", false},
		{"invalid_format_no_hyphens", "550e8400e29b41d4a716446655440000", false},
		{"wrong_hyphen_positions", "550e8400-e29b-41d4-a716-446655440000-wrong", false},
		{"invalid_characters", "550g8400-e29b-41d4-a716-446655440000", false},
		{"empty_string", "", false},
		{"too_long", "550e8400-e29b-41d4-a716-446655440000-extra", false},
		{"all_zeros", "00000000-0000-0000-0000-000000000000", true},
		{"all_fs", "ffffffff-ffff-ffff-ffff-ffffffffffff", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if result := isValidUUID(tt.input); result != tt.expected {
				t.Errorf("isValidUUID(%s) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestIsHexChar(t *testing.T) {
	tests := []struct {
		char     byte
		expected bool
	}{
		{'0', true},
		{'9', true},
		{'a', true},
		{'f', true},
		{'A', true},
		{'F', true},
		{'g', false},
		{'G', false},
		{'-', false},
		{' ', false},
		{'x', false},
	}

	for _, tt := range tests {
		name := string(tt.char)
		switch tt.char {
		case ' ':
			name = "space"
		case '-':
			name = "hyphen"
		}

		t.Run(name, func(t *testing.T) {
			if result := isHexChar(tt.char); result != tt.expected {
				t.Errorf("isHexChar(%c) = %v, want %v", tt.char, result, tt.expected)
			}
		})
	}
}

func TestCopyFile(t *testing.T) {
	tmpDir := MustCreateTempDir(t, "test-copy-")
	defer CleanupTempDir(tmpDir)

	sourceDir := filepath.Join(tmpDir, "source")
	destDir := filepath.Join(tmpDir, "dest")
	os.MkdirAll(sourceDir, 0755)
	os.MkdirAll(destDir, 0755)

	testContent := []byte("test content")
	sourceFile := filepath.Join(sourceDir, "test.txt")
	destFile := filepath.Join(destDir, "test.txt")

	if err := os.WriteFile(sourceFile, testContent, 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	if err := copyFile(sourceFile, destFile); err != nil {
		t.Fatalf("copyFile failed: %v", err)
	}

	copiedContent, err := os.ReadFile(destFile)
	if err != nil {
		t.Fatalf("Failed to read dest file: %v", err)
	}

	if !reflect.DeepEqual(copiedContent, testContent) {
		t.Errorf("File content mismatch: got %s, want %s", copiedContent, testContent)
	}
}

func TestCopyFile_NonExistentSource(t *testing.T) {
	tmpDir := MustCreateTempDir(t, "test-copy-")
	defer CleanupTempDir(tmpDir)

	sourceFile := filepath.Join(tmpDir, "nonexistent.txt")
	destFile := filepath.Join(tmpDir, "dest.txt")

	err := copyFile(sourceFile, destFile)
	if err == nil {
		t.Error("Expected error copying non-existent file, got nil")
	}
}

func TestCopyFile_OverwriteExisting(t *testing.T) {
	tmpDir := MustCreateTempDir(t, "test-copy-")
	defer CleanupTempDir(tmpDir)

	sourceFile := filepath.Join(tmpDir, "source.txt")
	destFile := filepath.Join(tmpDir, "dest.txt")

	oldContent := []byte("old content")
	newContent := []byte("new content")

	if err := os.WriteFile(sourceFile, newContent, 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}
	if err := os.WriteFile(destFile, oldContent, 0644); err != nil {
		t.Fatalf("Failed to create dest file: %v", err)
	}

	if err := copyFile(sourceFile, destFile); err != nil {
		t.Fatalf("copyFile failed: %v", err)
	}

	copiedContent, err := os.ReadFile(destFile)
	if err != nil {
		t.Fatalf("Failed to read dest file: %v", err)
	}

	if !reflect.DeepEqual(copiedContent, newContent) {
		t.Errorf("File content mismatch: got %s, want %s", copiedContent, newContent)
	}
}
