package gamedata

// LevelCap is the highest level.
func (d *Data) LevelCap() int { return d.p.LevelCap }

// ExpToNext is the EXP from level to level+1; false at the cap or for a level that doesn't exist.
func (d *Data) ExpToNext(level int) (int64, bool) {
	if level < 1 || level >= d.p.LevelCap {
		return 0, false
	}
	return d.p.ExpToNext[level-1], true
}
