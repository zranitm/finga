package itemroll_test

import (
	"reflect"
	"testing"

	"finga/internal/itemroll"
	"finga/internal/progression"
)

func TestEveryStatOfARollSurvivesTheTripThroughTheItemService(t *testing.T) {
	var roll progression.ItemStats
	v := reflect.ValueOf(&roll).Elem()
	for k := range v.NumField() {
		v.Field(k).SetInt(int64(k + 1)) // a different value in each, so a dropped or swapped stat shows
	}
	if got := itemroll.FromProto(itemroll.Proto(&roll)); got == nil || *got != roll {
		t.Errorf("%+v came back as %+v", roll, got)
	}
	if itemroll.Proto(nil) != nil || itemroll.FromProto(nil) != nil {
		t.Error("no roll should stay none")
	}
}
