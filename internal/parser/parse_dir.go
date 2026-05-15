package parser

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

var errMissingGlut = errors.New("file does not contain .glut metadata document")

func IsMissingGlut(err error) bool {
	return errors.Is(err, errMissingGlut)
}

// FileParseError holds a parse failure for a specific file.
type FileParseError struct {
	FilePath string
	Err      error
}

func (e *FileParseError) Error() string {
	return e.FilePath + ": " + e.Err.Error()
}

func (e *FileParseError) Unwrap() error {
	return e.Err
}

// ParseDir recursively finds all *.yml and *.yaml files and parses them.
func ParseDir(dirPath string) ([]*TestFile, []*FileParseError) {
	var files []*TestFile
	var errs []*FileParseError

	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			errs = append(errs, &FileParseError{FilePath: path, Err: err})
			return nil
		}

		ext := filepath.Ext(path)
		if !info.IsDir() && (ext == ".yml" || ext == ".yaml") {
			tf, parseErr := Parse(path)
			if parseErr != nil {
				if !errors.Is(parseErr, errMissingGlut) {
					errs = append(errs, &FileParseError{FilePath: path, Err: friendlyParseError(parseErr)})
				}
			} else {
				files = append(files, tf)
			}
		}
		return nil
	})

	if err != nil {
		errs = append(errs, &FileParseError{FilePath: dirPath, Err: err})
	}

	return files, errs
}

// friendlyParseError replaces yaml library internals with readable text.
func friendlyParseError(err error) error {
	var typeErr *yaml.TypeError
	if !errors.As(err, &typeErr) {
		return err
	}
	lines := make([]string, 0, len(typeErr.Errors))
	for _, msg := range typeErr.Errors {
		lines = append(lines, friendlyYAMLMessage(msg))
	}
	return errors.New(strings.Join(lines, "; "))
}

func friendlyYAMLMessage(msg string) string {
	replacements := [][2]string{
		{"cannot unmarshal !!map into string", "unexpected mapping where a string is expected (value may contain an unquoted ':')"},
		{"cannot unmarshal !!seq into string", "unexpected sequence where a string is expected"},
		{"cannot unmarshal !!str into ", "unexpected string where "},
		{"cannot unmarshal !!int into ", "unexpected integer where "},
		{"cannot unmarshal !!bool into ", "unexpected boolean where "},
	}
	for _, r := range replacements {
		msg = strings.ReplaceAll(msg, r[0], r[1])
	}
	return msg
}
