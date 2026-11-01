package git

import (
	"fmt"
	"path/filepath"
	"strings"
)

func (r Repo) FileHistoryPath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("file path cannot be empty")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	root, err := filepath.Abs(r.Root)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(root, absolute)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("file must be inside the repository")
	}
	return filepath.ToSlash(relative), nil
}
