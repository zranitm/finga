// Package api_test guards the shape of the contracts.
package api_test

import (
	"io/fs"
	"os"
	"strings"
	"testing"

	"google.golang.org/protobuf/reflect/protoregistry"

	_ "finga/api/gen/account/v1"
	_ "finga/api/gen/character/v1"
	_ "finga/api/gen/item/v1"
	_ "finga/api/gen/social/v1"
	_ "finga/api/gen/world/v1"
)

// A message one service's contract borrows from another's ties their releases together, so each
// contract declares its own.
func TestNoServiceContractImportsAnother(t *testing.T) {
	for _, path := range contracts(t) {
		f, err := protoregistry.GlobalFiles.FindFileByPath(path)
		if err != nil {
			t.Errorf("%s is not in this test; import its generated package above", path)
			continue
		}
		imports := f.Imports()
		for i := range imports.Len() {
			if imported := imports.Get(i).Path(); strings.HasPrefix(imported, "internal/") {
				t.Errorf("%s imports %s", path, imported)
			}
		}
	}
}

// contracts are the paths of the services' contracts, read from the proto tree so a new one is checked too.
func contracts(t *testing.T) []string {
	t.Helper()
	var paths []string
	err := fs.WalkDir(os.DirFS("proto"), "internal", func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, ".proto") {
			paths = append(paths, path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no contracts under proto/internal")
	}
	return paths
}
