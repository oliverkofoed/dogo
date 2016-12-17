package commandtree

import (
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/oliverkofoed/dogo/neaterror"
	"github.com/oliverkofoed/dogo/snobgob"
)

func TestStream(t *testing.T) {
	snobgob.Register(neaterror.Error{})
	snobgob.Register(RootCommand{})
	snobgob.Register(SleepCommand{})
	snobgob.Register(PrintMachineCommand{})
	snobgob.Register(execCommand{})

	root := NewRootCommand("tesing remote transfer")
	root.Add("print", NewExecCommand("", "", "", "echo", "hi there")).AsCommand().Add("hi below", &PrintMachineCommand{})
	/*s := &SleepCommand{Duration: time.Second}
	s2 := &SleepCommand{Duration: time.Second}
	root.Add("first sleep", s)
	s.Add("secondSleep", s2)*/

	fmt.Println("Hello")
	//fmt.Println("yousa")
	//fmt.Println("boosa")

	//root := commandtree.NewRootCommand("Get Agent State")
	//outer := commandtree.NewRootCommand("Get Agent State")
	//root.Add("get agent state", &registry.GetStateCommand{})
	root.Add("print", &PrintMachineCommand{})
	root.Add("sleep", &SleepCommand{Duration: 0 * time.Second}).AsCommand().Add("sleepmore", &SleepCommand{Duration: 0 * time.Second})

	r1, w1 := io.Pipe()
	r2, w2 := io.Pipe()

	newRoot := NewRootCommand("root")
	go func() {
		err := StreamCall(root, newRoot, 1, r1, nil, w2, func(s string) {
			newRoot.Logf(s)
		})
		w2.Close()
		newRoot.State = CommandStateCompleted
		if err != nil {
			panic(err)
		}
	}()

	go func() {
		err := StreamReceive(r2, w1)
		w1.Close()
		if err != nil {
			panic(err)
		}
	}()

	fmt.Println("BEFORE")
	ConsoleUI(newRoot)
	fmt.Println("DONE")
	//wg.Wait()
}
