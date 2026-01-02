package generator

// Generator Package Structure:
//
//   - generator.go      - Main entry point and various utilities
//   - types.go          - Type definitions, regex patterns, and embedded templates
//   - phase1.go         - File discovery, parsing, and metadata extraction
//   - phase2.go         - HTML page generation from org-mode files
//   - phase3.go         - Tag pages, index page, and static asset handling
//
// For full documentation, see github.com/niklasfasching/go-org

import (
	"os"
	"strings"
)

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}

func extractUUIDs(data []byte) []string {
	s := string(data)
	var uuids []string

	for i := 0; i < len(s); {
		idx := strings.Index(s[i:], ":ID:")
		if idx == -1 {
			break
		}

		idx += i + 4
		for idx < len(s) && s[idx] == ' ' {
			idx++
		}

		if idx+36 <= len(s) && isValidUUID(s[idx:idx+36]) {
			uuids = append(uuids, s[idx:idx+36])
			i = idx + 36
		} else {
			i = idx + 1
		}
	}
	return uuids
}

func isValidUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	if s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return false
	}
	for _, c := range s {
		if c != '-' && !isHexChar(byte(c)) {
			return false
		}
	}
	return true
}

func isHexChar(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}
