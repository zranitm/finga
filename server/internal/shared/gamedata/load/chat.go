package load

import "fmt"

const chatFile = "chat.yaml"

type chatYAML struct {
	LineMax int `yaml:"line_max"`
}

func (l *loader) loadChat(dir string) error {
	var c chatYAML
	if err := readYAML(dir, chatFile, &c); err != nil {
		return err
	}
	if c.LineMax <= 0 {
		return fmt.Errorf("%s: line_max must be positive", chatFile)
	}
	l.chatLineMax = c.LineMax
	return nil
}
