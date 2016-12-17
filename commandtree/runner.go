package commandtree

import (
	"sync"
	"time"
)

type Runner struct {
	root        CommandNode
	threads     int
	workChan    chan CommandNode
	initialized map[string]bool
	monitor     chan *MonitorEvent
	sync.Mutex
}

func NewRunner(root CommandNode, threads int) *Runner {
	return &Runner{
		root:        root,
		threads:     threads,
		initialized: make(map[string]bool),
		monitor:     make(chan *MonitorEvent, 10000),
	}
}

func (r *Runner) Run(monitor chan *MonitorEvent) bool {
	r.workChan = make(chan CommandNode, 100000)
	r.monitor = monitor

	// start work threads.
	for i := 0; i != r.threads; i++ {
		go func() {
			for command := range r.workChan {

				cmd := command.AsCommand()
				cmd.mutex.Lock()
				if cmd.State == CommandStateReady {
					cmd.State = CommandStateRunning
					if r.monitor != nil {
						r.monitor <- (&MonitorEvent{EventType: monitorEventStateChange, CommandID: cmd.ID, State: cmd.State})
						cmd.monitor = r.monitor
					}
					cmd.mutex.Unlock()
					command.Execute()
					cmd.mutex.Lock()
					if cmd.State == CommandStateRunning {
						cmd.State = CommandStateCompleted
					}
					if r.monitor != nil {
						r.monitor <- (&MonitorEvent{EventType: monitorEventStateChange, CommandID: cmd.ID, State: cmd.State})
					}
				}

				// check if any child commands can be started.
				if cmd.State == CommandStateCompleted && !cmd.anyError {
					for _, child := range cmd.Children {
						c := child.AsCommand()
						if c.State == CommandStateReady && !c.RemoteCommand {
							r.Lock()
							r.checkIfInitialized(c, cmd.ID)
							r.Unlock()
							r.workChan <- child
						}
					}
				}

				cmd.mutex.Unlock()
			}
		}()
	}

	// start a monitor thread to check for new commands and startable commands
	noError := false
	for true {
		r.Lock()
		done, s := r.check(r.root, true, "")
		noError = s
		if done {
			close(r.workChan)
		}
		r.Unlock()
		if done {
			break
		}
		time.Sleep(time.Millisecond * 20)
	}

	return noError
}

func (r *Runner) check(cmd CommandNode, startable bool, parentID string) (bool, bool) {
	c := cmd.AsCommand()

	done := c.State == CommandStateCompleted || c.State == CommandStatePaused
	noError := !c.anyError

	// known?
	r.checkIfInitialized(c, parentID)

	// can be started?
	if startable && c.State == CommandStateReady && !c.RemoteCommand {
		r.workChan <- cmd
	}

	// children?
	for _, child := range c.Children {
		childrenStartable := startable && (c.State == CommandStateCompleted || c.State == CommandStatePaused) && !c.anyError
		childrenDone, childrenNoError := r.check(child, childrenStartable, c.ID)
		done = (childrenDone || !childrenStartable) && done
		noError = childrenNoError && noError
	}

	return done, noError
}

func (r *Runner) checkIfInitialized(cmd *Command, parentID string) {
	if _, found := r.initialized[cmd.ID]; !found {
		if r.monitor != nil {
			r.monitor <- &MonitorEvent{EventType: monitorEventChildAdded, ParentID: parentID, CommandID: cmd.ID, Caption: cmd.Caption}
		}
		r.initialized[cmd.ID] = true
	}
}
