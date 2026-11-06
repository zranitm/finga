package application

import (
	"encoding/hex"
	"testing"

	"github.com/google/uuid"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/services/item/internal/model"
)

// A changed fingerprint refuses every retry stored before the deploy; a deliberate change bumps the version and value.
func TestFingerprintsAreStable(t *testing.T) {
	c, err := kernel.ParseCharacterID("0190a0f4-1c2d-7e3f-8a4b-5c6d7e8f9012")
	if err != nil {
		t.Fatal(err)
	}
	item, err := kernel.ParseItemID("0190a0f4-1c2d-7e3f-8a4b-5c6d7e8f9a0b")
	if err != nil {
		t.Fatal(err)
	}
	drop := uuid.MustParse("0190a0f4-1c2d-7e3f-8a4b-5c6d7e8f9c0d")
	for _, tc := range []struct {
		name string
		got  []byte
		want string
	}{
		{"add", AddInput{Key: "k-1", TemplateID: "slime_gel", Quantity: 3, Cause: model.Cause{Type: model.CauseDrop, ID: drop}}.fingerprint(c), "4b9139f2119c59ece19bb810e5be6562a12018c124918fb30695d0f444515826"},
		{"move", MoveInput{Key: "k-1", ItemID: item, Tab: gamedata.Use, Slot: 7}.fingerprint(c), "75b5e3b6161dbf0fe70935cb9e77fbb717c119ff92df45c1c9d951780f49e026"},
		{"discard", DiscardInput{Key: "k-1", ItemID: item, Quantity: 2}.fingerprint(c), "693b3c5bd5cd91ea7cd9157dead199a007206e0f3090fafb9ad8b11cfdaba75e"},
	} {
		if got := hex.EncodeToString(tc.got); got != tc.want {
			t.Errorf("%s fingerprint changed: retries made before this deploy would be refused\ngot  %s\nwant %s", tc.name, got, tc.want)
		}
	}
}

// The same input from another character is another request, and the key is not part of it.
func TestAFingerprintNamesTheCharacterNotTheKey(t *testing.T) {
	in := MoveInput{Key: "k-1", ItemID: kernel.NewItemID(), Tab: gamedata.Use, Slot: 7}
	c := kernel.NewCharacterID()
	if hex.EncodeToString(in.fingerprint(c)) == hex.EncodeToString(in.fingerprint(kernel.NewCharacterID())) {
		t.Fatal("two characters' moves share a fingerprint")
	}
	other := in
	other.Key = "k-2"
	if hex.EncodeToString(in.fingerprint(c)) != hex.EncodeToString(other.fingerprint(c)) {
		t.Fatal("the key changed the fingerprint")
	}
}
