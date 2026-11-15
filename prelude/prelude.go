// Package prelude holds the declarations that every Kogo program is
// checked against: spec.md sections 12 and 13.
package prelude

import (
	"embed"

	"kogo/syntax"
)

//go:embed *.kg
var source embed.FS

// Files parses the prelude. Each file is named prelude/<name>.kg.
func Files() ([]*syntax.File, error) {
	entries, err := source.ReadDir(".")
	if err != nil {
		return nil, err
	}
	var files []*syntax.File
	for _, e := range entries {
		src, err := source.ReadFile(e.Name())
		if err != nil {
			return nil, err
		}
		f, err := syntax.Parse("prelude/"+e.Name(), src)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, nil
}
