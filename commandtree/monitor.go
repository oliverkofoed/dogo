package commandtree

var SpinnerCharacters = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")

type monitorEventType int

const (
	monitorEventChildAdded  monitorEventType = iota // (parentID, commandId, caption)
	monitorEventStateChange                         // (commandID, newState)
	monitorEventLog                                 // (commandID, logMessage, logIsError, logTime)
	monitorEventResult                              // (commandID, result)
	monitorEventPanic                               // (Caption has the error)
)

type MonitorEvent struct {
	EventType monitorEventType
	CommandID string
	ParentID  string
	State     CommandState
	Caption   string
	LogEntry  *LogEntry
	Result    interface{}
}

/*
type ConsoleMonitor struct {
	refreshOutput bool
	root          *Command
	lookup        map[string]*Command
}

func NewConsoleMonitor(monitor chan *MonitorEvent) {
	c := &ConsoleMonitor{
		refreshOutput: true,
		lookup:        make(map[string]*Command),
	}

	height := 0
	ticker := time.NewTicker(time.Millisecond * 200)
	done := false
	spinIndex := 0
	for !done {
		select {
		case evt, received := <-monitor:
			if received {
				c.event(evt)
				c.refreshOutput = true
			} else {
				done = true
			}
		case _, received := <-ticker.C:
			if received && (c.refreshOutput || true) && term.IsTerminal {
				spinIndex++
				spinChar := string(spinnerCharacters[spinIndex%len(spinnerCharacters)])

				term.StartBuffer()
				terminalCols, _, _ := curse.GetScreenDimensions()
				c.clearTerminalBack(height)
				height = c.printStatus(c.root, 0, terminalCols, spinChar)
				term.Print(term.Reset)
				term.FlushBuffer(true)
				c.refreshOutput = false
			}
		}
	}
	c.clearTerminalBack(height)
	c.printLog(c.root, 0, "", "") //level int, prefix string)
	ticker.Stop()
}

func (c *ConsoleMonitor) clearTerminalBack(height int) {
	for i := 0; i != height; i++ {
		term.MoveUp(1)
		term.EraseCurrentLine()
	}
}

func (c *ConsoleMonitor) printStatus(cmd *Command, level int, terminalCols int, spinChar string) int {
	characters := 0
	for i := 0; i != level; i++ {
		if i == level-1 {
			if cmd.anyError {
				term.Print(term.Red + "! ")
				characters += 2
			} else {
				switch cmd.State {
				case CommandStateReady:
					term.Print(term.Reset + "+ ")
					characters += 2
					break
				case CommandStateRunning:
					term.Print(term.Yellow + spinChar + " ")
					characters += 2
					break
				case CommandStatePaused:
					term.Print(term.Reset + "* ")
					characters += 2
					break
				case CommandStateCompleted:
					term.Print(term.Green + "✓ ")
					characters += 2
					break
				}
			}
		} else {
			term.Print("  ")
			characters += 2
		}
	}

	if cmd.anyError {
		term.Print(term.Red + cmd.Caption + term.Reset)
		characters += len(cmd.Caption)
	} else {
		term.Print(term.White + cmd.Caption + term.Reset)
		characters += len(cmd.Caption)
	}

	var lastLogEntry *LogEntry
	if len(cmd.LogArray) > 0 {
		lastLogEntry = cmd.LogArray[len(cmd.LogArray)-1]
	}

	if lastLogEntry != nil {
		term.Print(": ")
		characters += 2
		for _, message := range strings.Split(lastLogEntry.Message, "\n") {
			if lastLogEntry.IsError {
				term.Print(term.Red + message)
				characters += len(message)
			} else {
				term.Print(term.Reset + message)
				characters += len(message)
			}
			break
		}
	}

	lines := int(math.Ceil(float64(characters) / float64(terminalCols)))
	term.Print("\n")

	for _, child := range cmd.Children {
		lines += c.printStatus(child.AsCommand(), level+1, terminalCols, spinChar)
	}

	return lines
}

func (c *ConsoleMonitor) printLog(cmd *Command, level int, prefix string, captionPath string) {
	if len(cmd.Log) > 0 || cmd.State != CommandStateCompleted {
		if level != 0 {
			if cmd.State != CommandStateCompleted {
				term.Print(prefix + term.Yellow + "[SKIPPED] " + fmt.Sprintf("%v", cmd.State) + term.White + term.White + captionPath + cmd.Caption + term.Reset)
			} else {
				term.Print(prefix + term.White + captionPath + cmd.Caption + term.Reset)
			}
			term.Print("\n")
		}
		for _, line := range cmd.log {
			for _, nline := range strings.Split(line.Message, "\n") {
				term.Print(prefix + "  ")
				if line.IsError {
					term.Print(term.Red + nline + term.Reset)
				} else {
					term.Print(nline)
				}
				term.Print("\n")
			}
		}
		if level != 0 {
			term.Print("\n")
		}

		prefix = prefix + "  "
	}

	for _, child := range cmd.Children {
		c.printLog(child.AsCommand(), level+1, prefix, captionPath+cmd.Caption+" > ")
	}
}

func (c *ConsoleMonitor) event(evt *MonitorEvent) {
	switch evt.EventType {
	case monitorEventChildAdded:
		if _, found := c.lookup[evt.CommandID]; !found {
			node := &Command{Caption: evt.Caption, State: CommandStateReady}
			if evt.ParentID != "" {
				parent, parentFound := c.lookup[evt.ParentID]
				if !parentFound {
					panic("could not find parent node." + evt.ParentID)
				}
				if parent.Children == nil {
					parent.Children = make([]CommandNode, 0, 5)
				}
				c := &commandWrapper{Command: *node}
				parent.Children = append(parent.Children, c)
				node = &c.Command
			} else {
				if c.root != nil {
					panic("already have a root node defined.")
				}
				c.root = node
			}
			c.lookup[evt.CommandID] = node
		}
		break
	case monitorEventStateChange:
		if node, found := c.lookup[evt.CommandID]; found {
			node.State = evt.State
		}
		break
	case monitorEventLog:
		if node, found := c.lookup[evt.CommandID]; found {
			if node.log == nil {
				node.log = make([]*LogEntry, 0, 10)
			}

			if evt.LogEntry.IsError {
				node.anyError = true
			}
			node.log = append(node.log, evt.LogEntry)
		}
		break
	}
}


*/
