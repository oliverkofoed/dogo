package commandtree

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/oliverkofoed/dogo/neaterror"
)

type CommandState int

const (
	CommandStateReady CommandState = iota
	CommandStateRunning
	CommandStatePaused
	CommandStateCompleted
)

type CommandNode interface {
	Execute()
	AsCommand() *Command
}

type Command struct {
	mutex         sync.RWMutex
	ID            string
	Caption       string
	State         CommandState
	monitor       chan *MonitorEvent
	LogArray      []*LogEntry
	anyError      bool
	Children      []CommandNode
	RemoteCommand bool
	result        interface{}
	progress      float64
}

func (c *Command) AnyError() bool {
	return c.anyError
}

func (c *Command) SetProgress(p float64) {
	c.progress = p
}

func (c *Command) GetResult() interface{} {
	return c.result
}

func (c *Command) SetResult(value interface{}) {
	c.result = value

	// tell monitor
	if c.monitor != nil {
		c.monitor <- &MonitorEvent{EventType: monitorEventResult, CommandID: c.ID, Result: c.result}
	}
}

type LogEntry struct {
	Message string
	Error   error
	Time    time.Time
}

func (c *Command) Add(caption string, cmd CommandNode) CommandNode {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.ID == "" {
		c.ID = makeID(c)
	}
	cx := cmd.AsCommand()
	cx.mutex.Lock()
	cx.Caption = caption
	cx.ID = makeID(cx)
	cx.mutex.Unlock()

	if c.Children == nil {
		c.Children = make([]CommandNode, 0, 0)
	}
	c.Children = append(c.Children, cmd)

	return cmd
}

func (c *Command) AsCommand() *Command {
	return c
}

// Err appends an error to the command execution log
func (c *Command) Err(err error) {
	if _, ok := err.(neaterror.Error); !ok {
		err = neaterror.New(nil, err.Error())
	}

	/*
		buf := bytes.NewBuffer(nil)
		errx := snobgob.NewEncoder(buf).Encode(err)
		if errx != nil {
			c.Logf("bad: %v, %v, %v", err, errx, string(debug.Stack()))
			return
		}*/
	c.log(&LogEntry{
		Error: err,
		Time:  time.Now(),
	})
}

// Errf appends a formatted error message to the commands execution log
func (c *Command) Errf(message string, args ...interface{}) {
	if len(args) > 0 {
		c.Err(fmt.Errorf(message, args...))
	} else {
		c.Err(errors.New(message))
	}
}

// Logf appends a formatted message to the commands execution log
func (c *Command) Logf(message string, args ...interface{}) {
	msg := message
	if len(args) > 0 {
		msg = fmt.Sprintf(message, args...)
	}
	c.log(&LogEntry{
		Message: msg,
		Time:    time.Now(),
	})
}

// Log appends a message to the commands execution log
func (c *Command) log(entry *LogEntry) {
	// save if log has any error
	if entry.Error != nil {
		c.anyError = true
	}

	// save log entry
	c.mutex.Lock()
	if c.LogArray == nil {
		c.LogArray = make([]*LogEntry, 0, 10)
	}

	c.LogArray = append(c.LogArray, entry)
	c.mutex.Unlock()

	// tell monitor
	if c.monitor != nil {
		c.monitor <- &MonitorEvent{EventType: monitorEventLog, CommandID: c.ID, LogEntry: entry}
	}
}

func makeID(cmd *Command) string {
	return fmt.Sprintf("%p", cmd)
}
