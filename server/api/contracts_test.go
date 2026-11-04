// Package api_test guards the shape of the contracts.
package api_test

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"

	accountv1 "finga/api/gen/account/v1"
	characterv1 "finga/api/gen/character/v1"
	itemv1 "finga/api/gen/item/v1"
	socialv1 "finga/api/gen/social/v1"
	worldv1 "finga/api/gen/world/v1"
)

// A message one service's contract borrows from another's ties their releases together, so each
// contract declares its own.
func TestNoServiceContractImportsAnother(t *testing.T) {
	for _, f := range []protoreflect.FileDescriptor{
		accountv1.File_internal_account_v1_account_proto,
		characterv1.File_internal_character_v1_character_proto,
		itemv1.File_internal_item_v1_item_proto,
		socialv1.File_internal_social_v1_social_proto,
		worldv1.File_internal_world_v1_world_proto,
	} {
		imports := f.Imports()
		for i := range imports.Len() {
			if path := imports.Get(i).Path(); strings.HasPrefix(path, "internal/") {
				t.Errorf("%s imports %s", f.Path(), path)
			}
		}
	}
}
