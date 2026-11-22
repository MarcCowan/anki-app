// Package archtest enforces package boundaries: SQL lives only in
// internal/store, and go-fsrs is imported only in internal/srs.
package archtest

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// moduleRoot walks up from the working directory until it finds go.mod.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

type importUse struct {
	file string // slash-separated path relative to the module root
	path string // imported package path
}

// imports returns every import of every non-test .go file in the module.
func imports(t *testing.T) []importUse {
	t.Helper()
	root := moduleRoot(t)
	var out []importUse
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if p != root && (strings.HasPrefix(name, ".") || name == "testdata" || name == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, p, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				return err
			}
			out = append(out, importUse{file: filepath.ToSlash(rel), path: path})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestOnlyStoreImportsSQL(t *testing.T) {
	for _, u := range imports(t) {
		if (u.path == "database/sql" || u.path == "modernc.org/sqlite" || strings.HasPrefix(u.path, "modernc.org/sqlite/")) &&
			!strings.HasPrefix(u.file, "internal/store/") {
			t.Errorf("%s imports %s; SQL belongs in internal/store only", u.file, u.path)
		}
	}
}

func TestOnlySrsImportsFSRS(t *testing.T) {
	for _, u := range imports(t) {
		if strings.HasPrefix(u.path, "github.com/open-spaced-repetition/go-fsrs") &&
			!strings.HasPrefix(u.file, "internal/srs/") {
			t.Errorf("%s imports %s; go-fsrs belongs in internal/srs only", u.file, u.path)
		}
	}
}
