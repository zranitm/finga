// Package till maps some of shop's errors to reasons, as a handler's table does.
package till

import (
	"errors"

	"google.golang.org/protobuf/types/descriptorpb"

	"finga/test/rules/testdata/errortables/shop"
)

type row struct {
	err    error
	reason descriptorpb.FieldDescriptorProto_Label
}

var rows = []row{
	{shop.ErrSoldOut, descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL},
	{errors.New("not shop's"), descriptorpb.FieldDescriptorProto_LABEL_REQUIRED},
}
