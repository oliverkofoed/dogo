package commandtree

import (
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"

	"sync"

	"github.com/oliverkofoed/dogo/neaterror"
	"github.com/oliverkofoed/dogo/term"
)

var statusPrinterInteruptQueue []func()
var statusPrinterInteruptLock sync.RWMutex

func ConsoleUIInterupt(fn func()) {
	statusPrinterInteruptLock.Lock()
	if statusPrinterInteruptQueue == nil {
		fn()
	} else {
		statusPrinterInteruptQueue = append(statusPrinterInteruptQueue, fn)
	}
	statusPrinterInteruptLock.Unlock()
	waiting := true
	for waiting {
		time.Sleep(time.Millisecond * 100)
		statusPrinterInteruptLock.Lock()
		waiting = len(statusPrinterInteruptQueue) > 0
		statusPrinterInteruptLock.Unlock()
	}
}

func startInteruptable() {
	statusPrinterInteruptLock.Lock()
	statusPrinterInteruptQueue = make([]func(), 0)
	statusPrinterInteruptLock.Unlock()
}

func endInteruptable() {
	statusPrinterInteruptLock.Lock()
	for _, fn := range statusPrinterInteruptQueue {
		fn()
	}
	statusPrinterInteruptQueue = nil
	statusPrinterInteruptLock.Unlock()
}

func runInterupts(pre func()) {
	statusPrinterInteruptLock.Lock()
	pre()
	for _, fn := range statusPrinterInteruptQueue {
		fn()
	}
	statusPrinterInteruptQueue = statusPrinterInteruptQueue[0:0]
	statusPrinterInteruptLock.Unlock()
}

type consoleUI struct {
	root *Command
}

func ConsoleUI(root CommandNode) error {
	c := &consoleUI{
		root: root.AsCommand(),
	}

	height := 0
	done := false
	spinIndex := 0
	_, _, dimensionErr := term.GetDimensions()
	canStatusPrint := term.IsTerminal && dimensionErr == nil

	startInteruptable()
	for {
		// pick spinner
		spinIndex++
		spinChar := string(SpinnerCharacters[spinIndex%len(SpinnerCharacters)])

		// print output
		term.StartBuffer()
		terminalCols, _, _ := term.GetDimensions()

		if canStatusPrint {
			c.clearTerminalBack(height)
		}

		// allow interuptions
		runInterupts(func() {
			if canStatusPrint {
				term.FlushBuffer(true)
			}
		})

		height, done = c.printStatus(c.root, 0, terminalCols, spinChar)
		if done {
			if canStatusPrint {
				c.clearTerminalBack(height)
				term.FlushBuffer(true)
			} else {
				term.FlushBuffer(false)
			}
			break
		}
		term.Print(term.Reset)
		term.FlushBuffer(canStatusPrint)
		time.Sleep(time.Millisecond * 200)
	}
	endInteruptable()

	c.printLog(c.root, 0, "")

	errs := make([]string, 0)
	c.getErrors(root.AsCommand(), "", &errs)
	if len(errs) > 0 {
		s := "s"
		if len(errs) == 1 {
			s = ""
		}
		fmt.Println(term.Red + "====================================")
		fmt.Println(term.Red + fmt.Sprintf("             %v error%v", len(errs), s))
		fmt.Println(term.Red + "====================================" + term.Reset)
		return fmt.Errorf("%v errors during run", len(errs))
	}
	return nil
}

func (c *consoleUI) clearTerminalBack(height int) {
	for i := 0; i != height; i++ {
		term.MoveUp(1)
		term.EraseCurrentLine()
	}
}

func (c *consoleUI) getErrors(cmd *Command, path string, arr *[]string) {
	if path != "" {
		path += term.Reset + " -> "
	}
	path += term.Red + cmd.Caption

	if cmd.anyError {
		*arr = append(*arr, path)
	}

	for _, child := range cmd.Children {
		c.getErrors(child.AsCommand(), path, arr)
	}
}

func (c *consoleUI) printStatus(cmd *Command, level int, terminalCols int, spinChar string) (int, bool) {
	characters := 0
	for i := 0; i != level; i++ {
		if i == level-1 {
			if cmd.anyError {
				tprint(term.Red+"! ", &characters)
			} else {
				switch cmd.State {
				case CommandStateReady:
					tprint(term.Reset+"+ ", &characters)
					break
				case CommandStateRunning:
					tprint(term.Yellow+spinChar+" ", &characters)
					break
				case CommandStatePaused:
					tprint(term.Reset+"Ⅱ ", &characters)
					break
				case CommandStateCompleted:
					tprint(term.Green+"✓ ", &characters)
					break
				}
			}
		} else {
			tprint("  ", &characters)
		}
	}

	if cmd.anyError {
		tprint(cmd.Caption+term.Reset, &characters)
	} else {
		tprint(cmd.Caption+term.Reset, &characters)
	}

	var lastLogEntry *LogEntry
	cmd.mutex.RLock()
	if len(cmd.LogArray) > 0 {
		lastLogEntry = cmd.LogArray[len(cmd.LogArray)-1]
	}
	cmd.mutex.RUnlock()

	if lastLogEntry != nil {
		tprint(": ", &characters)
		color := term.Reset
		message := lastLogEntry.Message
		if lastLogEntry.Error != nil {
			color = term.Red
			if n, ok := lastLogEntry.Error.(neaterror.Error); ok {
				message = n.Prefix + n.Message
			} else {
				message = lastLogEntry.Error.Error()
			}
		}
		message = strings.Replace(message, "\n", "", -1)
		message = strings.Replace(message, "\r", "", -1)
		tprint(color+message, &characters)
	}

	if cmd.progress != 0 {
		tprint(fmt.Sprintf(": %4.2f%%", cmd.progress*100), &characters)
	}

	lines := int(math.Ceil(float64(characters) / float64(terminalCols)))
	tprint("\n", &characters)
	done := cmd.State == CommandStateCompleted

	for _, child := range cmd.Children {
		childLines, childDone := c.printStatus(child.AsCommand(), level+1, terminalCols, spinChar)
		lines += childLines
		done = done && childDone
	}

	return lines, done
}

func tprint(input string, count *int) {
	c := 0

	skipTo := rune(-1)
	for n, i := range input {
		if skipTo != -1 {
			if i == skipTo {
				skipTo = -1
			}
			continue
		}
		if i == 27 { // start of code
			r := input[n+1:]
			if len(r) >= 3 && r[0] == 91 {
				skipTo = 109
				continue
			}
		}
		if unicode.IsControl(i) {
			continue
		}

		c++
	}

	*count += c

	term.Print(input)
}

func (c *consoleUI) printLog(cmd *Command, level int, prefix string) {
	captionPrefix := ""
	if level != 0 {
		if cmd.anyError {
			captionPrefix = prefix + (term.Red + "! " + term.RedBold)
		} else {
			switch cmd.State {
			case CommandStateReady:
				captionPrefix = prefix + (term.Reset + "+ ")
				break
			case CommandStateRunning:
				captionPrefix = prefix + (term.Yellow + ". ")
				break
			case CommandStatePaused:
				captionPrefix = prefix + (term.Reset + "Ⅱ ")
				break
			case CommandStateCompleted:
				captionPrefix = prefix + (term.Green + "✓ ")
				break
			}
		}
	} else if level == 0 {
		captionPrefix = term.Reset
	}
	term.Print(captionPrefix + cmd.Caption + term.Reset)
	term.Print("\n")
	for _, line := range cmd.LogArray {
		message := line.Message
		color := ""
		if line.Error != nil {
			color = term.Red
			message = neaterror.String("", line.Error, true)
		}
		for _, nline := range strings.Split(message, "\n") {
			term.Print(prefix)
			term.Print("  ")
			term.Print(color)
			term.Print(nline)
			term.Print(term.Reset)
			term.Print("\n")
		}
	}

	if level != 0 {
		prefix = prefix + "  "
	}
	for _, child := range cmd.Children {
		c.printLog(child.AsCommand(), level+1, prefix)
	}
}

func SingleCommandUI(c *Command) {
	term.Print(term.Reset + c.Caption + term.Reset + "\n")
	logPtr := 0
	flush := func() {
		c.mutex.Lock()
		for ; logPtr < len(c.LogArray); logPtr++ {
			line := c.LogArray[logPtr]
			message := line.Message
			color := ""
			if line.Error != nil {
				color = term.Red
				message = neaterror.String("", line.Error, true)
			}
			for _, nline := range strings.Split(message, "\n") {
				term.Print("  ")
				term.Print(color)
				term.Print(nline)
				term.Print(term.Reset)
				term.Print("\n")
			}
		}
		c.mutex.Unlock()
	}
	startInteruptable()
	for c.State != CommandStatePaused && c.State != CommandStateCompleted {
		flush()
		time.Sleep(time.Millisecond * 20)
		runInterupts(func() {})
	}
	endInteruptable()
	flush()
}
