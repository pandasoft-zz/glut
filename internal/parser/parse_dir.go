package parser

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var errMissingGlut = errors.New("file does not contain glut: key")

// ParseDir recursively finds all *.yml and *.yaml files and parses them.
func ParseDir(dirPath string) ([]*TestFile, []error) {
	var files []*TestFile
	var errs []error

	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			errs = append(errs, err)
			return nil
		}

		ext := filepath.Ext(path)
		if !info.IsDir() && (ext == ".yml" || ext == ".yaml") {
			tf, parseErr := Parse(path)
			if parseErr != nil {
				if !errors.Is(parseErr, errMissingGlut) {
					errs = append(errs, fmt.Errorf("%s: %w", path, parseErr))
				}
			} else {
				files = append(files, tf)
			}
		}
		return nil
	})

	if err != nil {
		errs = append(errs, err)
	}

	return files, errs
}
