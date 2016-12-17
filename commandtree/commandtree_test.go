package commandtree

import (
	"os"
	"testing"
	"time"
)

func TestCommandTree(t *testing.T) {
	root := NewRootCommand("tesing remote transfer")
	root.Add("print", &PrintMachineCommand{})
	s := &SleepCommand{Duration: time.Second}
	s2 := &SleepCommand{Duration: time.Second}
	s2.Errf("This is my error")
	s2.Logf("This is goods")
	root.Add("first sleep", s)
	s.Add("secondSleep", s2)

	r := NewRunner(root, 1)
	go r.Run(nil)
	ConsoleUI(root)
}

type PrintMachineCommand struct {
	Command
}

func (c *PrintMachineCommand) Execute() {
	n, err := os.Hostname()
	c.Logf("%v, %v", n, err)
}

type SleepCommand struct {
	Command
	Duration time.Duration
}

func (c *SleepCommand) Execute() {
	time.Sleep(c.Duration)
	c.Logf("Slept %v", c.Duration)
}
