# C15. Target lock

- **Feature:** C15. Target lock
- **Group:** C. Combat and progression
- **Priority:** P1
- **Status:** Agreed

**Story:** As a player, I lock onto a mob and my attacks and skills go to it, wherever I'm facing or walking. I can switch to the next mob with one key, so I fight a pack on my own terms and move freely while I do.

## Acceptance

- **Auto-lock:**
  - With nothing locked, the game locks the nearest living mob within 12 m, measured across the ground. It may be in any direction, behind the player included.
  - The lock stays on that mob until it dies, goes beyond 12 m, or the player switches. A mob that comes closer doesn't take it.
  - When the lock ends, the nearest mob is locked again at once.
  - Walls and ledges don't hide a mob from the lock, as they don't hide players from mobs.
- **Switching:**
  - Tab, in both presets, locks the next closest mob: the nearest one farther than the current lock.
  - After the farthest, it wraps round to the nearest.
  - It counts mobs in every direction within 12 m.
- **Attacks and skills go to the lock:**
  - A basic attack or skill aims at the locked mob, not the way the player faces.
  - With nothing locked, it goes the way the player faces.
  - A target out of the action's reach is still aimed at, so the swing or shot goes toward it and misses. The player closes in.
  - An arc or an area still hits every mob it covers, not only the locked one.
  - Heal works as before.
- **Facing:**
  - A basic attack turns the player toward the lock for its swing only. Once the swing has struck, they face the way they walk again.
  - A skill turns them for its whole second, as today.
- **Showing the lock:**
  - The locked mob has a gold ring on the ground under it, plus the gold outline that marked the auto-aim's pick before.
  - Only the player sees their own lock.
- **Unchanged:** the server still decides every hit from the aim it's sent, and walking already lets a player face any way. So a lock gives no reach or power a player couldn't get by turning.

## Not in this feature

- Locking with the mouse, or locking players and NPCs.
- A target frame with the mob's name and HP.
- Bosses taking the lock first: the lock goes by distance.
- Seeing other players' locks.

## Rules

Systems → Combat → Aiming, and Screens and controls.
