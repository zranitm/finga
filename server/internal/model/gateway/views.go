package gateway

// PlayerView is a player as everyone on its map sees it.
type PlayerView struct {
	ID, Name, Class string
	Level           int
	X, Y, Z         float64
	VX, VY, VZ      float64
	HX, HZ          float64 // the unit heading
	OnGround        bool
	HP, MaxHP       int
	Dead            bool
	Invulnerable    bool    // for the hit guard after a mob's hit
	Aim             float32 // the yaw of its latest action
	Move            PlayerMove
	Dodging         bool // while a skill's dodge runs
	Shield          int  // what's left of its shields
	Held            int  // sub-steps left of a status holding it
	Slow            PlayerSlow
	Statuses        []StatusView
	Immune          uint64 // the tick its hard immunity ends on
}

// PlayerSlow is a status slowing a player's walk: Factor × the walk speed for Left more sub-steps; Left 0 is none.
type PlayerSlow struct {
	Left   int
	Factor float64
}

// StatusView is an abnormal status on a player or mob: its id and the tick it ends on, the latest of its kind's.
type StatusView struct {
	ID   string
	Ends uint64
}

// PlayerMove is a skill's dash or leap under way: (VX, VZ) replaces the walk for Left more sub-steps, and with Stop it ends at a mob's body; Left 0 is none.
type PlayerMove struct {
	VX, VZ float64
	Left   int
	Stop   bool
}

// MobView is a mob as the players on its map see it; its ID counts up within one map instance.
type MobView struct {
	ID        uint64
	Type      string
	X, Y, Z   float64
	Yaw       float32
	HP, MaxHP int
	Pose      MobPose
	Statuses  []StatusView
	Immune    uint64 // the tick its hard immunity ends on
}

// MobPose is how a mob takes the blows that land on it.
type MobPose int

// The poses: standing, stopped a moment, knocked down, and up again in super armor.
const (
	Standing MobPose = iota
	Flinching
	Down
	Rising
)

// DropView is an item on the ground; its ID counts up within one map instance.
type DropView struct {
	ID       uint64
	Template string
	Quantity int
	X, Y, Z  float64
	Owner    string // the character it flies to
}

// CharacterView is the player's own character.
type CharacterView struct {
	ID, Name, Class string
	Level           int
	Exp             int64
	Stats           Stats
	HP, MaxHP       int
	Energy          int // as it joins; each Delta carries it after
	Sheet           StatSheet
}

// StatSheet is the character's stats with what it wears, and the equipment's share of each.
type StatSheet struct {
	Primary, PrimaryWorn, PrimarySkills Stats
	Derived, DerivedWorn, DerivedSkills DerivedStats
	Points                              Points
	Skills                              SkillPoints
	AttackLengthTicks                   int
}

// SkillPoints are the skill points: unspent, and invested in each skill by ID.
type SkillPoints struct {
	Unspent  int
	Invested map[string]int
}

// Points are the stat points: unspent, spent in each stat, and whether the free reset is left.
type Points struct {
	Unspent       int
	Spent         Stats
	FreeResetLeft bool
}

// DerivedStats are the computed stats: rates, crit damage and penetration as fractions, regen in points a second.
type DerivedStats struct {
	PhysicalAttack, MagicAttack, PhysicalDefense, MagicDefense float64
	Accuracy, Evasion, CritRate, CritDamage, Penetration       float64
	HPRegen                                                    float64
	CooldownSpeed, MoveSpeed                                   float64
	MaxHP                                                      int
}

// ItemView is an item in the player's inventory, or a worn one, which is in no tab.
type ItemView struct {
	ID, Template, Tab string
	Slot, Quantity    int
	Stats             *ItemStats // equipment's roll; nil for anything else
}

// ItemStats are what one equipment item gives: the five primary stats and the thirteen derived, rates, crit damage and penetration in tenths of a percent, regen in tenths of a point a second.
type ItemStats struct {
	STR, DEX, INT, AGI, VIT                                    int
	PhysicalAttack, MagicAttack, PhysicalDefense, MagicDefense int
	MaxHP, Accuracy, Evasion                                   int
	CritRate, CritDamage, Penetration, HPRegen                 int
	CooldownSpeed, MoveSpeed                                   int
}

// EquipSlot is an equip slot and what it holds; Item is nil when it is empty.
type EquipSlot struct {
	Slot string
	Item *ItemView
}

// Inventory is the player's items and what it wears as they stood at Revision, the count of their changes.
type Inventory struct {
	Revision  uint64
	Items     []ItemView
	Equipment []EquipSlot
}

// InventorySlot is a slot an inventory command changed; Item is nil when the slot is now empty.
type InventorySlot struct {
	Tab  string
	Slot int
	Item *ItemView
}

// PartyView is the player's party.
type PartyView struct {
	Leader  string
	Members []PartyMember
}

// PartyMember is one member of a party, online or not.
type PartyMember struct {
	CharacterID, Name, Class string
	Level, HP, MaxHP         int
	MapID                    string
	Online                   bool
}

// Entity is a player (Character set) or a mob.
type Entity struct {
	Character string
	Mob       uint64
}

// ChatChannel is where a chat line goes; the zero value is no channel, which the social module refuses.
type ChatChannel int

// The chat channels.
const (
	ChatMap ChatChannel = iota + 1
	ChatParty
)
