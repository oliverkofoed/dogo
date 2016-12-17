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
	//root.Add("printing", &registry.SleepCommand{Duration: time.Second})
	root.Add("first sleep", s)
	s.Add("secondSleep", s2)

	/*return
	root := NewRootCommand("Doing a bunch of stuff")
	root.Add("Say Hello", &PrintCommand{Message: "hello world"})
	root.Add("Sleep for a second", &SleepCommand{Duration: time.Second * 1})

	sleep := &SleepCommand{Duration: time.Second * 1}
	root.Add("Sleep a little", sleep)
	sleep.Add("Print Yahoho", &PrintCommand{Message: "Sub that should not start!"})
	*/

	r := NewRunner(root, 1)
	go r.Run(nil)
	ConsoleUI(root)
	//NewConsoleMonitor(r.Monitor)
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
