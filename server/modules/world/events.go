package world

import (
	"slices"
	"time"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/internal/sim"
	"finga/modules/world/internal/application/instance"
	"finga/modules/world/internal/model"
	"finga/pkg/hub"
)

// deliverQueue bounds a gateway's stream: about two seconds of frames for a few hundred players.
const deliverQueue = 8192

// Delivery is one event on a gateway's stream: for one player through one connection, or a Frame for several.
type Delivery struct {
	Character kernel.CharacterID // zero for a Frame
	Conn      string
	Tick      uint64 // the map's tick when it happened; 0 for Detached
	Event     Event
}

// Event is something a map tells a player.
//
//sumtype:decl
type Event interface{ worldEvent() }

// Snapshot is the whole map, sent to a player on join.
type Snapshot struct {
	MapID     string
	Players   []PlayerView
	Mobs      []MobView
	Drops     []DropView
	FieldBoss *FieldBoss // while one is up
}

// Frame is what changed on a map in one tick, once for every player there through the gateway; read-only.
type Frame struct {
	Players        []PlayerView
	Mobs           []MobView
	Drops          []DropView // new on the ground
	Removed        []kernel.CharacterID
	RemovedMobs    []uint64
	RemovedDrops   []uint64 // gone without an owner taking them: expired or lost
	CollectedDrops []uint64 // left the ground for their owners' inventories
	Recipients     []Recipient
}

// Recipient is a player a frame goes to, with AckSeq, the latest input applied for it, and its energy as of then when it changed or the player just joined.
type Recipient struct {
	Character  kernel.CharacterID
	Connection string
	AckSeq     uint32
	Energy     *int
}

// Teleport moves the player to another map, whose snapshot follows.
type Teleport struct {
	MapID string
	X, Y  float64
}

// Detached says a connection's character is on no map any more, as a failed transfer leaves it; the gateway joins it again.
type Detached struct{}

// Damage is one resolved hit, sent to everyone on the map.
type Damage struct {
	Source EntityRef
	Target EntityRef
	Amount int
	Crit   bool
	Miss   bool
}

// Died says a character fell, to everyone on the map.
type Died struct {
	ID   kernel.CharacterID
	X, Y float64
}

// Respawned says a character is back, to the map it left and to the character.
type Respawned struct {
	ID    kernel.CharacterID
	MapID string
	X, Y  float64
	HP    int
}

// ExpGained is EXP awarded to the player, after the award.
type ExpGained struct {
	Amount int64
	Exp    int64
}

// LevelUp carries the player's new level, own stats, sheet with the new points, and restored HP; energy stays as it was.
type LevelUp struct {
	ID    kernel.CharacterID
	Name  string
	Class kernel.Class
	Level int
	Exp   int64
	Stats gamedata.Stats
	HP    int
	MaxHP int
	Sheet Sheet
}

// Promoted is the player in its new class, as a level-up leaves it.
type Promoted LevelUp

// StatsChanged is the player's HP after its gear, points, regen or a potion moved it.
type StatsChanged struct {
	Sheet *Sheet // set when the sheet changed; nil when only HP moved
	HP    int
	MaxHP int
}

// SkillUsed says a character used a skill at a stage, facing a side, for drawing; its damage comes as Damage.
type SkillUsed struct {
	Caster kernel.CharacterID
	Skill  string
	Stage  int
	Facing int
}

// AttackUsed says a character's basic attack started, facing a side, lasting Length ticks, for drawing.
type AttackUsed struct {
	Attacker kernel.CharacterID
	Facing   int
	Length   uint64
}

// MobAttacked says a mob's attack began its windup, an index into its type's attacks, for drawing.
type MobAttacked struct {
	Mob    uint64
	Attack int
	Facing int
	To     float64 // where its hit goes: a leap's landing or a rush's end; for other kinds, the mob's x
}

// FieldBoss is a map's field boss, up for LeavesIn unless beaten.
type FieldBoss struct {
	Mob      uint64
	Type     string
	LeavesIn time.Duration
}

// FieldBossGone says the field boss left, or was beaten.
type FieldBossGone struct {
	Mob    uint64
	Beaten bool
}

// Chat is a map chat line.
type Chat struct {
	Sender     kernel.CharacterID
	SenderName string
	Text       string
}

// Loot tells a player what became of a drop: their own, or a party member's they saw collected.
type Loot struct {
	Kind      LootKind
	MapID     string // drop IDs are the map's own
	DropID    uint64
	Owner     kernel.CharacterID
	OwnerName string
	Template  string
	Quantity  int
}

// LootKind is what became of a drop.
type LootKind int

// The kinds of loot, as the model counts them.
const (
	LootCollected LootKind = iota + 1 // in its owner's inventory
	LootWaiting                       // on the ground until its owner has room
	LootLost                          // gone at the end of its life, never collected
)

// InventoryChanged tells the player which slots and equip slots a committed change touched, at the inventory's Revision.
type InventoryChanged struct {
	Revision  uint64
	Slots     []Slot
	Equipment []EquipSlot
}

// NpcDialog is what an NPC within reach offers the player.
type NpcDialog struct {
	NPC       string
	Gives     []QuestOffer
	Takes     []QuestTurnIn
	Promotion *PromotionOffer // set when the NPC trains a class whose parent is the character's
}

// QuestChanged is one quest of the player's log after a change.
type QuestChanged struct {
	ID     string
	Change QuestChange
	Quest  Quest
}

// QuestChange is what happened to a quest.
type QuestChange uint8

// The quest changes, as the model counts them.
const (
	QuestProgressed QuestChange = iota + 1
	QuestAccepted
	QuestHandedIn
	QuestAbandoned
)

func (Snapshot) worldEvent()         {}
func (Frame) worldEvent()            {}
func (Teleport) worldEvent()         {}
func (Detached) worldEvent()         {}
func (Damage) worldEvent()           {}
func (Died) worldEvent()             {}
func (Respawned) worldEvent()        {}
func (ExpGained) worldEvent()        {}
func (LevelUp) worldEvent()          {}
func (Promoted) worldEvent()         {}
func (StatsChanged) worldEvent()     {}
func (SkillUsed) worldEvent()        {}
func (AttackUsed) worldEvent()       {}
func (MobAttacked) worldEvent()      {}
func (FieldBoss) worldEvent()        {}
func (FieldBossGone) worldEvent()    {}
func (Chat) worldEvent()             {}
func (Loot) worldEvent()             {}
func (InventoryChanged) worldEvent() {}
func (NpcDialog) worldEvent()        {}
func (QuestChanged) worldEvent()     {}

// PlayerView is a player as everyone on the map sees it.
type PlayerView struct {
	ID           kernel.CharacterID
	Name         string
	Class        kernel.Class
	Level        int
	HP           int
	MaxHP        int
	Dead         bool
	Invulnerable bool // the hit guard after a mob's hit
	State        sim.State
}

// MobView is a mob as everyone on the map sees it.
type MobView struct {
	ID     uint64
	Type   string
	X, Y   float64 // feet centre
	Facing int
	HP     int
	MaxHP  int
	Pose   MobPose
}

// MobPose is how a mob takes the blows that land on it.
type MobPose int

// The poses, as the model counts them: standing, stopped a moment, knocked down, and up again in super armor.
const (
	Standing MobPose = iota
	Flinching
	Down
	Rising
)

// DropView is an item on the ground as everyone sees it, with the player it will fly to.
type DropView struct {
	ID       uint64
	Template string
	Quantity int
	X, Y     float64
	Owner    kernel.CharacterID
}

// EntityRef names a player or a mob in a combat event.
type EntityRef struct {
	Player kernel.CharacterID // zero when the entity is a mob
	Mob    uint64
}

// IsMob reports whether the reference names a mob.
func (e EntityRef) IsMob() bool { return e.Player.IsZero() }

// Slot is one inventory slot a change touched; Item is nil when it emptied.
type Slot struct {
	Tab  gamedata.Tab
	Slot int
	Item *SlotItem
}

// SlotItem is the item in a slot after the change.
type SlotItem struct {
	ID       kernel.ItemID
	Template string
	Quantity int
	Stats    *progression.ItemStats // what it rolled; nil for anything but equipment
}

// EquipSlot is one equip slot a change touched; Item is nil when it emptied.
type EquipSlot struct {
	Slot string
	Item *SlotItem
}

// QuestOffer is a quest an NPC gives, and whether the player may take it.
type QuestOffer struct {
	Quest string
	State OfferState
	Unmet Unmet
}

// OfferState is whether an offered quest can be taken.
type OfferState uint8

// The offer states, as the model counts them.
const (
	OfferAvailable OfferState = iota + 1
	OfferActive
	OfferFinished // a one-time quest done
	OfferUnmet    // its conditions aren't met yet
)

// Unmet is what an offered quest still asks for.
type Unmet struct {
	Level  int          // the least level, when the character's is lower
	Class  kernel.Class // the class asked for, when the character is another
	Quests []string     // quests to finish first
}

// QuestTurnIn is a quest the NPC takes back, and whether it is ready.
type QuestTurnIn struct {
	Quest string
	Ready bool
}

// PromotionOffer is the class an NPC trains, and each condition of it.
type PromotionOffer struct {
	Class      kernel.Class
	Conditions []Condition
}

// Condition is one thing a promotion asks, met when Have reaches Need.
type Condition struct {
	Kind ConditionKind
	ID   string // the stat, skill or quest; empty for the level
	Need int
	Have int
}

// ConditionKind is what a promotion's condition counts.
type ConditionKind uint8

// The condition kinds, as the model counts them.
const (
	ConditionLevel ConditionKind = iota + 1
	ConditionStat                // own stat points, without gear
	ConditionSkill               // points invested in the skill
	ConditionQuest               // finished: need 1, and have 1 once it is
)

// Subscribe opens a gateway's stream; a newer one under the same ID closes it, as a full queue does, and a closed one drops its players.
func (m *Module) Subscribe(gateway string) *hub.Sub[Delivery] { return m.deliver.Subscribe(gateway) }

// sink is how a map tells one player, through the connection it joined by, what happens.
type sink struct {
	sub       *hub.Sub[Delivery]
	character kernel.CharacterID
	conn      string
}

var _ instance.Sink = sink{}

// Stream is the player's gateway's stream, which every player joined through that gateway shares.
func (s sink) Stream() instance.Stream { return stream{sub: s.sub} }

// Send queues one event for the player; false drops the player.
func (s sink) Send(e model.Event) bool {
	d := deliveryOf(e)
	if d.Event == nil {
		return true // an event the facade has no name for: nothing to send
	}
	d.Character, d.Conn = s.character, s.conn
	return s.sub.Send(d)
}

// stream is one gateway's stream.
type stream struct{ sub *hub.Sub[Delivery] }

// SendFrame queues the frame once for all its recipients on the gateway; false drops every recipient.
func (g stream) SendFrame(f *model.Frame, to []model.Recipient) bool {
	return g.sub.Send(Delivery{Tick: f.Tick, Event: frameOf(f, to)})
}

// frameOf is the map's frame as the facade's own: its lists are copies, small and mostly empty, so no module shares the map's buffers.
func frameOf(f *model.Frame, to []model.Recipient) Frame {
	out := Frame{
		Players: playerViews(f.Players), Mobs: mobViews(f.Mobs), Drops: dropViews(f.Drops),
		Removed: slices.Clone(f.Removed), RemovedMobs: slices.Clone(f.RemovedMobs), RemovedDrops: slices.Clone(f.RemovedDrops), CollectedDrops: slices.Clone(f.CollectedDrops),
		Recipients: make([]Recipient, len(to)),
	}
	for i, r := range to {
		out.Recipients[i] = Recipient(r)
	}
	return out
}

// deliveryOf is a map's event as its gateway hears it, with its tick; no event for one it has no name for.
func deliveryOf(e model.Event) Delivery {
	switch ev := e.(type) {
	case model.Snapshot:
		snap := Snapshot{MapID: ev.MapID, Players: playerViews(ev.Players), Mobs: mobViews(ev.Mobs), Drops: dropViews(ev.Drops)}
		if b := ev.FieldBoss; b != nil {
			snap.FieldBoss = &FieldBoss{Mob: b.Mob, Type: b.Type, LeavesIn: b.LeavesIn}
		}
		return Delivery{Tick: ev.Tick, Event: snap}
	case model.FieldBoss:
		return Delivery{Tick: ev.Tick, Event: FieldBoss{Mob: ev.Mob, Type: ev.Type, LeavesIn: ev.LeavesIn}}
	case model.FieldBossGone:
		return Delivery{Tick: ev.Tick, Event: FieldBossGone{Mob: ev.Mob, Beaten: ev.Beaten}}
	case model.Teleport:
		return Delivery{Tick: ev.Tick, Event: Teleport{MapID: ev.MapID, X: ev.X, Y: ev.Y}}
	case model.Damage:
		return Delivery{Tick: ev.Tick, Event: Damage{Source: EntityRef(ev.Source), Target: EntityRef(ev.Target), Amount: ev.Amount, Crit: ev.Crit, Miss: ev.Miss}}
	case model.SkillUsed:
		return Delivery{Tick: ev.Tick, Event: SkillUsed{Caster: ev.Caster, Skill: ev.Skill, Stage: ev.Stage, Facing: ev.Facing}}
	case model.AttackUsed:
		return Delivery{Tick: ev.Tick, Event: AttackUsed{Attacker: ev.Attacker, Facing: ev.Facing, Length: ev.Length}}
	case model.MobAttacked:
		return Delivery{Tick: ev.Tick, Event: MobAttacked{Mob: ev.Mob, Attack: ev.Attack, Facing: ev.Facing, To: ev.To}}
	case model.Died:
		return Delivery{Tick: ev.Tick, Event: Died{ID: ev.ID, X: ev.X, Y: ev.Y}}
	case model.Respawned:
		return Delivery{Tick: ev.Tick, Event: Respawned{ID: ev.ID, MapID: ev.MapID, X: ev.X, Y: ev.Y, HP: ev.HP}}
	case model.ExpGained:
		return Delivery{Tick: ev.Tick, Event: ExpGained{Amount: ev.Amount, Exp: ev.Exp}}
	case model.LevelUp:
		return Delivery{Tick: ev.Tick, Event: levelUpOf(ev)}
	case model.Promoted:
		return Delivery{Tick: ev.Tick, Event: Promoted(levelUpOf(model.LevelUp(ev)))}
	case model.StatsChanged:
		sc := StatsChanged{HP: ev.HP, MaxHP: ev.MaxHP}
		if ev.Sheet != nil {
			s := sheetOf(*ev.Sheet)
			sc.Sheet = &s
		}
		return Delivery{Tick: ev.Tick, Event: sc}
	case model.Chat:
		return Delivery{Tick: ev.Tick, Event: Chat{Sender: ev.Sender, SenderName: ev.SenderName, Text: ev.Text}}
	case model.InventoryChanged:
		return Delivery{Tick: ev.Tick, Event: InventoryChanged{Revision: ev.Revision, Slots: slotsOf(ev.Slots), Equipment: equipSlotsOf(ev.Equipment)}}
	case model.Loot:
		return Delivery{Tick: ev.Tick, Event: Loot{
			Kind: LootKind(ev.Kind), MapID: ev.MapID, DropID: ev.DropID, Owner: ev.Owner, OwnerName: ev.OwnerName, Template: ev.Template, Quantity: ev.Quantity,
		}}
	case model.Detached:
		return Delivery{Event: Detached{}}
	case model.NpcDialog:
		return Delivery{Tick: ev.Tick, Event: npcDialogOf(ev)}
	case model.QuestChanged:
		return Delivery{Tick: ev.Tick, Event: QuestChanged{ID: ev.ID, Change: QuestChange(ev.Change), Quest: questOf(ev.Quest)}}
	}
	return Delivery{}
}

func levelUpOf(ev model.LevelUp) LevelUp {
	return LevelUp{
		ID: ev.ID, Name: ev.Name, Class: ev.Class, Level: ev.Level, Exp: ev.Exp, Stats: ev.Stats,
		HP: ev.HP, MaxHP: ev.MaxHP, Sheet: sheetOf(ev.Sheet),
	}
}

func playerViews(ps []model.PlayerView) []PlayerView {
	if len(ps) == 0 {
		return nil
	}
	out := make([]PlayerView, len(ps))
	for i, p := range ps {
		out[i] = PlayerView(p)
	}
	return out
}

func mobViews(ms []model.MobView) []MobView {
	if len(ms) == 0 {
		return nil
	}
	out := make([]MobView, len(ms))
	for i, m := range ms {
		out[i] = MobView{ID: m.ID, Type: m.Type, X: m.X, Y: m.Y, Facing: m.Facing, HP: m.HP, MaxHP: m.MaxHP, Pose: MobPose(m.Pose)}
	}
	return out
}

func dropViews(ds []model.DropView) []DropView {
	if len(ds) == 0 {
		return nil
	}
	out := make([]DropView, len(ds))
	for i, d := range ds {
		out[i] = DropView(d)
	}
	return out
}

func slotsOf(in []model.Slot) []Slot {
	out := make([]Slot, 0, len(in))
	for _, s := range in {
		out = append(out, Slot{Tab: s.Tab, Slot: s.Slot, Item: (*SlotItem)(s.Item)})
	}
	return out
}

func equipSlotsOf(in []model.EquipSlot) []EquipSlot {
	if len(in) == 0 {
		return nil
	}
	out := make([]EquipSlot, 0, len(in))
	for _, s := range in {
		out = append(out, EquipSlot{Slot: s.Slot, Item: (*SlotItem)(s.Item)})
	}
	return out
}

func npcDialogOf(d model.NpcDialog) NpcDialog {
	out := NpcDialog{NPC: d.NPC}
	for _, o := range d.Gives {
		out.Gives = append(out.Gives, QuestOffer{Quest: o.Quest, State: OfferState(o.State), Unmet: Unmet(o.Unmet)})
	}
	for _, t := range d.Takes {
		out.Takes = append(out.Takes, QuestTurnIn(t))
	}
	if p := d.Promotion; p != nil {
		out.Promotion = &PromotionOffer{Class: p.Class}
		for _, c := range p.Conditions {
			out.Promotion.Conditions = append(out.Promotion.Conditions, Condition{Kind: ConditionKind(c.Kind), ID: c.ID, Need: c.Need, Have: c.Have})
		}
	}
	return out
}
