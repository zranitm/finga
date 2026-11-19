package world

import (
	"slices"
	"time"

	"finga/internal/application/world/instance"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/progression"
	"finga/internal/shared/sim"
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
	MapID         string
	Players       []PlayerView
	Mobs          []MobView
	Pouches       []PouchView    // the joiner's own
	FieldBoss     *FieldBoss     // while one is up
	FieldBossGone *FieldBossGone // while the map's is beaten
}

// Frame is what changed on a map in one tick, once for every player there through the gateway; read-only.
type Frame struct {
	Players     []PlayerView
	Mobs        []MobView
	Removed     []kernel.CharacterID
	RemovedMobs []uint64
	Recipients  []Recipient
}

// Recipient is a player a frame goes to, with AckSeq, the latest input applied for it, its energy as of then when it changed or the player just joined, and its own pouches that changed, which nobody else's frame carries.
type Recipient struct {
	Character  kernel.CharacterID
	Connection string
	AckSeq     uint32
	Energy     *int
	Pouches    Pouches
}

// Pouches are the changes to a player's own pouches in a frame: those new or changed, and those gone.
type Pouches struct {
	Changed []PouchView
	Gone    []uint64
}

// Teleport moves the player to another map, whose snapshot follows.
type Teleport struct {
	MapID string
	X, Y  float64
	Z     float64
	Yaw   float32
}

// Detached says a connection's character is on no map any more, as a failed transfer leaves it; the gateway joins it again.
type Detached struct{}

// Damage is one resolved hit, sent to everyone on the map.
type Damage struct {
	Source   EntityRef
	Target   EntityRef
	Amount   int
	Crit     bool
	Miss     bool
	Dodge    bool   // a mob's attack passing through a player's dodge
	Block    bool   // a mob's attack a player's block stopped
	Status   string // a damage-over-time tick's status
	Shielded int    // what of Amount a player's shields took
}

// Died says a character fell, to everyone on the map.
type Died struct {
	ID   kernel.CharacterID
	X, Y float64
	Z    float64
}

// Respawned says a character is back, to the map it left and to the character.
type Respawned struct {
	ID    kernel.CharacterID
	MapID string
	X, Y  float64
	HP    int
	Z     float64
	Yaw   float32
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
	Sheet  *Sheet // set when the sheet changed; nil when only HP moved
	HP     int
	MaxHP  int
	Timers *SkillTimers // set when they replace every cooldown and buff the player holds: into and out of a match, and at each round's reset
}

// MatchState is a match as its fighters see it, on entry and at each change.
type MatchState struct {
	Match  kernel.MatchID
	Mode   string
	Phase  MatchPhase
	Round  int
	Wins   [2]int
	EndsIn time.Duration
	Sides  [2][]MatchFighter
}

// MatchPhase is where a match stands.
type MatchPhase int

// The phases: before the first round, counting into a round, fighting it, between rounds, and the outcome.
const (
	PhasePrep MatchPhase = iota
	PhaseCountdown
	PhaseFight
	PhaseBetween
	PhaseResult
)

// MatchFighter is a fighter as the match lists them.
type MatchFighter struct {
	ID    kernel.CharacterID
	Name  string
	Class kernel.Class
	Bot   bool
}

// SkillUsed says a character used a skill at a stage, with an extra trait or its default, aimed along a yaw, for drawing; its damage comes as Damage.
type SkillUsed struct {
	Caster kernel.CharacterID
	Skill  string
	Stage  int
	Trait  string // "" for the default
	Aim    float32
	Target EntityRef // the lock it was aimed at, zero for none
}

// AttackUsed says a character's basic attack started, aimed along a yaw, lasting Length ticks, for drawing.
type AttackUsed struct {
	Attacker kernel.CharacterID
	Length   uint64
	Aim      float32
}

// MobAttacked says a mob's attack began its windup, an index into its type's attacks, turned to Yaw, for drawing.
type MobAttacked struct {
	Mob           uint64
	Attack        int
	Yaw           float32
	ToX, ToY, ToZ float64 // where its hit goes: a leap's landing or a rush's end; for other kinds, where the mob stands
}

// FieldBoss is a map's field boss, up until it's beaten.
type FieldBoss struct {
	Mob  uint64
	Type string
}

// FieldBossGone says the field boss is beaten, and how long until it's back.
type FieldBossGone struct {
	Mob    uint64
	Type   string
	BackIn time.Duration
}

// Chat is a map chat line.
type Chat struct {
	Sender     kernel.CharacterID
	SenderName string
	Text       string
}

// Loot tells a player what became of a pouch's items: their own taken or left, or a party member's taken.
type Loot struct {
	Kind      LootKind
	MapID     string // pouch IDs are the map's own
	Pouch     uint64
	Owner     kernel.CharacterID
	OwnerName string
	Items     []LootItem
}

// LootItem is an item a Loot names.
type LootItem struct {
	Template string
	Quantity int
}

// TeleportCancelled tells the player their teleport channel ended before it took them, and why.
type TeleportCancelled struct {
	Reason TeleportCancel
}

// TeleportCancel is what ended a teleport channel early.
type TeleportCancel int

// The causes, as the model counts them: a movement or jump key, an attack or skill starting, a mob's landed hit, and death.
const (
	TeleportMoved TeleportCancel = iota + 1
	TeleportActed
	TeleportHit
	TeleportDied
)

// LootKind is what became of a pouch's items.
type LootKind int

// The kinds of loot, as the model counts them.
const (
	LootTaken LootKind = iota + 1 // in the taker's inventory
	LootLeft                      // still in the pouch when its life ended
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

func (Snapshot) worldEvent()          {}
func (Frame) worldEvent()             {}
func (Teleport) worldEvent()          {}
func (TeleportCancelled) worldEvent() {}
func (Detached) worldEvent()          {}
func (Damage) worldEvent()            {}
func (Died) worldEvent()              {}
func (Respawned) worldEvent()         {}
func (ExpGained) worldEvent()         {}
func (LevelUp) worldEvent()           {}
func (Promoted) worldEvent()          {}
func (StatsChanged) worldEvent()      {}
func (MatchState) worldEvent()        {}
func (SkillUsed) worldEvent()         {}
func (AttackUsed) worldEvent()        {}
func (MobAttacked) worldEvent()       {}
func (FieldBoss) worldEvent()         {}
func (FieldBossGone) worldEvent()     {}
func (Chat) worldEvent()              {}
func (Loot) worldEvent()              {}
func (InventoryChanged) worldEvent()  {}
func (NpcDialog) worldEvent()         {}
func (QuestChanged) worldEvent()      {}

// PlayerView is a player as everyone on the map sees it.
type PlayerView struct {
	ID           kernel.CharacterID
	Name         string
	Class        kernel.Class
	Level        int
	HP           int
	MaxHP        int
	Dead         bool
	Invulnerable bool      // the hit guard after a mob's hit
	Dodging      bool      // a skill's dodge, mobs' attacks passing through
	Shield       int       // what's left of its shield buffs' pools
	State        sim.State // Held and Slow carry its statuses' hold on its movement
	Aim          float32   // the yaw its latest attack or skill aimed along
	Statuses     []StatusView
	Team         int // its side in a match, 1 or 2; 0 outside one
	Bot          bool
	Look         Look
}

// Look is a player's five picks, each a piece id, "" keeping the class's own, at the count of its changes.
type Look struct {
	Outfit       string
	Headwear     string
	Face         string
	Weapon       string
	Wings        string
	HideHeadwear bool
	Revision     uint64
}

// StatusView is an abnormal status on a player or mob: its id and the tick it ends on, the latest of its kind's.
type StatusView struct {
	ID   string
	Ends uint64
}

// MobView is a mob as everyone on the map sees it.
type MobView struct {
	ID       uint64
	Type     string
	X, Y, Z  float64 // feet centre, y up
	Yaw      float32
	HP       int
	MaxHP    int
	Pose     MobPose
	Statuses []StatusView // its abnormal statuses
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

// PouchView is a pouch as its owner alone sees it, gone after GoneIn.
type PouchView struct {
	ID      uint64
	Kind    PouchKind
	X, Y, Z float64
	Items   []PouchItem
	GoneIn  time.Duration
}

// PouchKind is what a pouch is.
type PouchKind int

// The pouch kinds, as the model counts them.
const (
	PouchSmall PouchKind = iota + 1 // a kill's
	PouchBox                        // a boss's, one per eligible player
)

// PouchItem is an item in a pouch; ID is its own within the pouch.
type PouchItem struct {
	ID       uint32
	Template string
	Quantity int
	Stats    *progression.ItemStats // equipment's roll, nil for anything else
}

// Pick is one item a take asks for.
type Pick struct {
	Pouch uint64
	Item  uint32
}

// PickAnswer is what a pick came to: taken when Err is nil, else refused with why.
type PickAnswer struct {
	Pick
	Err error
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
		Players: playerViews(f.Players), Mobs: mobViews(f.Mobs),
		Removed: slices.Clone(f.Removed), RemovedMobs: slices.Clone(f.RemovedMobs),
		Recipients: make([]Recipient, len(to)),
	}
	for i, r := range to {
		out.Recipients[i] = Recipient{
			Character: r.Character, Connection: r.Connection, AckSeq: r.AckSeq, Energy: r.Energy,
			Pouches: Pouches{Changed: pouchViews(r.Pouches.Changed), Gone: slices.Clone(r.Pouches.Gone)},
		}
	}
	return out
}

// deliveryOf is a map's event as its gateway hears it, with its tick; no event for one it has no name for.
func deliveryOf(e model.Event) Delivery {
	switch ev := e.(type) {
	case model.Snapshot:
		snap := Snapshot{MapID: ev.MapID, Players: playerViews(ev.Players), Mobs: mobViews(ev.Mobs), Pouches: pouchViews(ev.Pouches)}
		if b := ev.FieldBoss; b != nil {
			snap.FieldBoss = &FieldBoss{Mob: b.Mob, Type: b.Type}
		}
		if b := ev.FieldBossGone; b != nil {
			snap.FieldBossGone = &FieldBossGone{Mob: b.Mob, Type: b.Type, BackIn: b.BackIn}
		}
		return Delivery{Tick: ev.Tick, Event: snap}
	case model.FieldBoss:
		return Delivery{Tick: ev.Tick, Event: FieldBoss{Mob: ev.Mob, Type: ev.Type}}
	case model.FieldBossGone:
		return Delivery{Tick: ev.Tick, Event: FieldBossGone{Mob: ev.Mob, Type: ev.Type, BackIn: ev.BackIn}}
	case model.Teleport:
		return Delivery{Tick: ev.Tick, Event: Teleport{MapID: ev.MapID, X: ev.X, Y: ev.Y, Z: ev.Z, Yaw: ev.Yaw}}
	case model.Damage:
		return Delivery{Tick: ev.Tick, Event: Damage{Source: EntityRef(ev.Source), Target: EntityRef(ev.Target), Amount: ev.Amount, Crit: ev.Crit, Miss: ev.Miss, Dodge: ev.Dodge, Block: ev.Block, Status: ev.Status, Shielded: ev.Shielded}}
	case model.SkillUsed:
		return Delivery{Tick: ev.Tick, Event: SkillUsed{Caster: ev.Caster, Skill: ev.Skill, Stage: ev.Stage, Trait: ev.Trait, Aim: ev.Aim, Target: EntityRef(ev.Target)}}
	case model.AttackUsed:
		return Delivery{Tick: ev.Tick, Event: AttackUsed{Attacker: ev.Attacker, Length: ev.Length, Aim: ev.Aim}}
	case model.MobAttacked:
		return Delivery{Tick: ev.Tick, Event: MobAttacked{Mob: ev.Mob, Attack: ev.Attack, Yaw: ev.Yaw, ToX: ev.ToX, ToY: ev.ToY, ToZ: ev.ToZ}}
	case model.Died:
		return Delivery{Tick: ev.Tick, Event: Died{ID: ev.ID, X: ev.X, Y: ev.Y, Z: ev.Z}}
	case model.Respawned:
		return Delivery{Tick: ev.Tick, Event: Respawned{ID: ev.ID, MapID: ev.MapID, X: ev.X, Y: ev.Y, HP: ev.HP, Z: ev.Z, Yaw: ev.Yaw}}
	case model.ExpGained:
		return Delivery{Tick: ev.Tick, Event: ExpGained{Amount: ev.Amount, Exp: ev.Exp}}
	case model.TeleportCancelled:
		return Delivery{Tick: ev.Tick, Event: TeleportCancelled{Reason: TeleportCancel(ev.Reason)}}
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
		if ev.Timers != nil {
			t := timersOf(*ev.Timers)
			sc.Timers = &t
		}
		return Delivery{Tick: ev.Tick, Event: sc}
	case model.MatchState:
		st := MatchState{Match: ev.Match, Mode: ev.Mode, Phase: MatchPhase(ev.Phase), Round: ev.Round, Wins: ev.Wins, EndsIn: ev.EndsIn}
		for s, side := range ev.Sides {
			for _, f := range side {
				st.Sides[s] = append(st.Sides[s], MatchFighter(f))
			}
		}
		return Delivery{Tick: ev.Tick, Event: st}
	case model.Chat:
		return Delivery{Tick: ev.Tick, Event: Chat{Sender: ev.Sender, SenderName: ev.SenderName, Text: ev.Text}}
	case model.InventoryChanged:
		return Delivery{Tick: ev.Tick, Event: InventoryChanged{Revision: ev.Revision, Slots: slotsOf(ev.Slots), Equipment: equipSlotsOf(ev.Equipment)}}
	case model.Loot:
		items := make([]LootItem, len(ev.Items))
		for i, it := range ev.Items {
			items[i] = LootItem(it)
		}
		return Delivery{Tick: ev.Tick, Event: Loot{Kind: LootKind(ev.Kind), MapID: ev.MapID, Pouch: ev.Pouch, Owner: ev.Owner, OwnerName: ev.OwnerName, Items: items}}
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
		out[i] = PlayerView{
			ID: p.ID, Name: p.Name, Class: p.Class, Level: p.Level, HP: p.HP, MaxHP: p.MaxHP, Dead: p.Dead,
			Invulnerable: p.Invulnerable, Dodging: p.Dodging, Shield: p.Shield, State: p.State, Aim: p.Aim,
			Statuses: statusViews(p.Statuses), Team: p.Team, Bot: p.Bot, Look: Look(p.Look),
		}
	}
	return out
}

func statusViews(ss []model.StatusView) []StatusView {
	if len(ss) == 0 {
		return nil
	}
	out := make([]StatusView, len(ss))
	for i, s := range ss {
		out[i] = StatusView(s)
	}
	return out
}

func mobViews(ms []model.MobView) []MobView {
	if len(ms) == 0 {
		return nil
	}
	out := make([]MobView, len(ms))
	for i, m := range ms {
		out[i] = MobView{
			ID: m.ID, Type: m.Type, X: m.X, Y: m.Y, Z: m.Z, Yaw: m.Yaw, HP: m.HP, MaxHP: m.MaxHP, Pose: MobPose(m.Pose),
			Statuses: statusViews(m.Statuses),
		}
	}
	return out
}

func pouchViews(ps []model.PouchView) []PouchView {
	if len(ps) == 0 {
		return nil
	}
	out := make([]PouchView, len(ps))
	for i, p := range ps {
		items := make([]PouchItem, len(p.Items))
		for k, it := range p.Items {
			items[k] = PouchItem(it)
		}
		out[i] = PouchView{ID: p.ID, Kind: PouchKind(p.Kind), X: p.X, Y: p.Y, Z: p.Z, Items: items, GoneIn: p.GoneIn}
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
