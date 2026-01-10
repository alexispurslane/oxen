package generator

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestGenerateAtomFeed_Empty(t *testing.T) {
	tmpDir := MustCreateTempDir(t, "test-atom-")
	defer CleanupTempDir(tmpDir)

	procFiles := &ProcessedFiles{
		Files:   []FileInfo{},
		UuidMap: sync.Map{},
		TagMap:  sync.Map{},
	}

	ctx := BuildContext{
		Root:         tmpDir,
		DestDir:      tmpDir,
		ForceRebuild: true,
		SiteName:     "Test Site",
	}

	_, _, _, atomTmpl, _, err := SetupTemplates("/nonexistent")
	if err != nil {
		t.Fatalf("SetupTemplates() error = %v", err)
	}

	result := GenerateAtomFeed(procFiles, ctx, atomTmpl)

	if result.Errors != 0 {
		t.Errorf("GenerateAtomFeed() errors = %v, want 0", result.Errors)
	}

	feedPath := filepath.Join(tmpDir, "feed.xml")
	if _, err := os.Stat(feedPath); os.IsNotExist(err) {
		t.Error("GenerateAtomFeed() did not create feed.xml")
	}
}
