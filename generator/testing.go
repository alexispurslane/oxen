package generator

import (
	"os"
	"path/filepath"
	"time"
)

func CreateTestOrgFile(dir, filename, content string) error {
	path := filepath.Join(dir, filename)
	return os.WriteFile(path, []byte(content), 0644)
}

func CreateTestDirStructure(base string, dirs []string) error {
	for _, dir := range dirs {
		path := filepath.Join(base, dir)
		if err := os.MkdirAll(path, 0755); err != nil {
			return err
		}
	}
	return nil
}

func CreateTestFileWithModTime(dir, filename string, content []byte, modTime time.Time) error {
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, content, 0644); err != nil {
		return err
	}
	return os.Chtimes(path, modTime, modTime)
}

func CreateTestBuildContext(root, dest, siteName string, forceRebuild bool) *BuildContext {
	return &BuildContext{
		Root:         root,
		DestDir:      dest,
		ForceRebuild: forceRebuild,
		TmplModTime:  time.Time{},
		SiteName:     siteName,
	}
}

func CleanupTempDir(dir string) {
	os.RemoveAll(dir)
}

func CreateTestTemplateFile(dir, name, content string) error {
	path := filepath.Join(dir, name)
	return os.WriteFile(path, []byte(content), 0644)
}
