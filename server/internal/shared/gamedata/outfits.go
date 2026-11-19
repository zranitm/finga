package gamedata

// WeaponKind is what a class holds, which a weapon look must be of.
type WeaponKind string

const (
	OneHanded WeaponKind = "one_handed"
	Knife     WeaponKind = "knife"
	Crossbow  WeaponKind = "crossbow"
	Staff     WeaponKind = "staff"
)

// LookSlot is one of the five picks a look makes.
type LookSlot string

const (
	OutfitSlot   LookSlot = "outfit"
	HeadwearSlot LookSlot = "headwear"
	FaceSlot     LookSlot = "face"
	WeaponSlot   LookSlot = "weapon"
	WingsSlot    LookSlot = "wings" // none is the class's own
)

// Piece is a wardrobe entry: a look for one slot, never stats (outfits.md section 3).
type Piece struct {
	ID    string
	Slot  LookSlot
	Kind  WeaponKind // weapon pieces alone
	Name  string
	Stars int
	Look  string // a key in the client's look table
	Item  string // the item that holds it, PieceItem(ID), when a boss drops it
	From  []PieceSource
}

// PieceSource is where a piece comes from: a boss's drop, at a chance on each eligible player's own roll.
type PieceSource struct {
	Boss   string
	Chance float64
}

// PieceItem is the id of the item that holds the piece id.
func PieceItem(id string) string { return "piece." + id }

// Pieces lists the wardrobe's pieces in file order.
func (d *Data) Pieces() []Piece { return d.p.Pieces }

// Piece finds a piece.
func (d *Data) Piece(id string) (Piece, bool) { return find(d.p.Pieces, d.pieceIdx, id) }

// PieceIn finds the piece an item holds.
func (d *Data) PieceIn(item string) (Piece, bool) { return find(d.p.Pieces, d.pieceItemIdx, item) }
