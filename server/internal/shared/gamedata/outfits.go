package gamedata

// WeaponKind is what a class holds, which a weapon look must be of.
type WeaponKind string

// The weapon kinds.
const (
	OneHanded WeaponKind = "one_handed"
	Knife     WeaponKind = "knife"
	Crossbow  WeaponKind = "crossbow"
	Staff     WeaponKind = "staff"
)

// LookSlot is one of the five picks a look makes.
type LookSlot string

// The look slots.
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

// PieceSource is where a piece comes from: a boss's drop, at a chance on each eligible player's own roll; a PvP rank, the first time the account reaches it; a quest's hand-in; or a feat.
type PieceSource struct {
	Boss   string
	Chance float64
	Rank   string
	Quest  string
	Feat   string
}

// Feat is something an account does that gives pieces the first time; When is the one condition the server sees as it happens (outfits.md section 3).
type Feat struct {
	ID   string
	Name string
	When FeatWhen
}

// FeatWhen is a feat's one condition: a level-up to Level or past it, a field boss's rewards (Boss, or AnyBoss), a promotion, Quest's hand-in, or a PvP win that brings the character's wins over every mode to PvpWins or past it.
type FeatWhen struct {
	Level    int
	Boss     string
	Promoted bool
	Quest    string
	PvpWins  int
}

// AnyBoss is a boss feat's condition met by every field boss.
const AnyBoss = "any"

// PieceItem is the id of the item that holds the piece id.
func PieceItem(id string) string { return "piece." + id }

// Pieces lists the wardrobe's pieces in file order.
func (d *Data) Pieces() []Piece { return d.p.Pieces }

// Piece finds a piece.
func (d *Data) Piece(id string) (Piece, bool) { return find(d.p.Pieces, d.pieceIdx, id) }

// PieceIn finds the piece an item holds.
func (d *Data) PieceIn(item string) (Piece, bool) { return find(d.p.Pieces, d.pieceItemIdx, item) }

// RankPiece finds the piece a PvP rank gives, false for none.
func (d *Data) RankPiece(rank string) (Piece, bool) { return find(d.p.Pieces, d.rankPieceIdx, rank) }

// Feats lists the feats in file order.
func (d *Data) Feats() []Feat { return d.p.Feats }

// FeatPieces is the pieces feat gives, in file order.
func (d *Data) FeatPieces(feat string) []string { return d.featPieces[feat] }
