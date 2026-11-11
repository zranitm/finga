package load

import "fmt"

const expCurveFile = "exp_curve.yaml"

type expCurveYAML struct {
	LevelCap  int `yaml:"level_cap"`
	ExpToNext []struct {
		Level int   `yaml:"level"`
		Exp   int64 `yaml:"exp"`
	} `yaml:"exp_to_next"`
}

func (l *loader) loadExpCurve(dir string) error {
	var file expCurveYAML
	if err := readYAML(dir, expCurveFile, &file); err != nil {
		return err
	}
	if file.LevelCap < 2 {
		return fmt.Errorf("%s: level_cap must be at least 2", expCurveFile)
	}
	l.levelCap = file.LevelCap
	l.expToNext = make([]int64, file.LevelCap-1)
	seen := make([]bool, file.LevelCap-1)
	for _, e := range file.ExpToNext {
		if e.Level < 1 || e.Level >= file.LevelCap {
			return fmt.Errorf("%s: level %d is at or past the cap %d", expCurveFile, e.Level, file.LevelCap)
		}
		if seen[e.Level-1] {
			return fmt.Errorf("%s: duplicate level %d", expCurveFile, e.Level)
		}
		if e.Exp < 1 {
			return fmt.Errorf("%s: level %d must need positive EXP", expCurveFile, e.Level)
		}
		seen[e.Level-1] = true
		l.expToNext[e.Level-1] = e.Exp
	}
	for level := 1; level < file.LevelCap; level++ {
		if !seen[level-1] {
			return fmt.Errorf("%s: no entry for level %d", expCurveFile, level)
		}
		if level > 1 && l.expToNext[level-1] <= l.expToNext[level-2] {
			return fmt.Errorf("%s: level %d must need more EXP than level %d", expCurveFile, level, level-1)
		}
	}
	return nil
}
