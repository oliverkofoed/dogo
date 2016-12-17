package commandtree

import (
	"bytes"
	"io"
	"os/exec"
	"time"
)

// ---- RootCommand ----

func NewRootCommand(caption string) *RootCommand {
	r := &RootCommand{Command{Caption: caption}}
	r.Command.ID = makeID(&r.Command)
	return r
}

type RootCommand struct {
	Command
}

func (c *RootCommand) Execute() {}

// ---- FuncCommand ----

type funcCommand struct {
	Command
	execute func(cmd *Command)
}

func NewFuncCommand(execute func(cmd *Command)) CommandNode {
	return &funcCommand{execute: execute}
}

func (c *funcCommand) Execute() {
	c.execute(&c.Command)
}

// ---- BashCommands ----

type bashCommands struct {
	Command
	Dir             string
	LogPrefix       string
	TimeTakenFormat string
	Commands        []string
}

func NewBashCommands(dir string, timeTakenFormat string, logprefix string, commands ...string) CommandNode {
	return &bashCommands{
		Dir:             dir,
		LogPrefix:       logprefix,
		TimeTakenFormat: timeTakenFormat,
		Commands:        commands,
	}
}

func (c *bashCommands) Execute() {
	start := time.Now()
	for _, cmd := range c.Commands {
		cmd := exec.Command("/bin/bash", "-c", cmd)
		cmd.Stdout = NewLogFuncWriter(c.LogPrefix, c.Logf)
		cmd.Stderr = NewLogFuncWriter(c.LogPrefix, c.Errf)
		if err := cmd.Run(); err != nil {
			c.Err(err)
			return
		}
	}
	if c.TimeTakenFormat != "" {
		c.Logf(c.TimeTakenFormat, time.Since(start))
	}
}

// ---- ExecCommand ----

type execCommand struct {
	Command
	Dir             string
	Program         string
	Args            []string
	LogPrefix       string
	TimeTakenFormat string
}

func NewExecCommand(dir string, timeTakenFormat string, logprefix string, program string, args ...string) CommandNode {
	return &execCommand{
		Dir:             dir,
		Program:         program,
		Args:            args,
		LogPrefix:       logprefix,
		TimeTakenFormat: timeTakenFormat,
	}
}

func (c *execCommand) Execute() {
	start := time.Now()
	err := OSExec(&c.Command, c.Dir, c.LogPrefix, c.Program, c.Args...)
	if err != nil {
		c.Err(err)
	}
	if c.TimeTakenFormat != "" {
		c.Logf(c.TimeTakenFormat, time.Since(start))
	}
}

func OSExec(owner *Command, dir string, logPrefix string, program string, args ...string) error {
	cmd := exec.Command(program, args...)
	cmd.Dir = dir
	cmd.Stdout = NewLogFuncWriter(logPrefix, owner.Logf)
	cmd.Stderr = NewLogFuncWriter(logPrefix, owner.Errf)
	return cmd.Run()
}

func NewLogFuncWriter(prefix string, log func(format string, args ...interface{})) io.Writer {
	return &commandLogWriter{
		log:    log,
		prefix: prefix,
		buf:    bytes.NewBuffer(nil),
	}
}

type commandLogWriter struct {
	prefix string
	buf    *bytes.Buffer
	log    func(format string, args ...interface{})
}

func (w *commandLogWriter) Write(p []byte) (n int, err error) {
	// write to buffer
	w.buf.Write(p)

	// find string in buffer
	for {
		arr := w.buf.Bytes()

		// find \n
		lineEnds := -1
		lineExtra := 0
		for i, chr := range arr {
			if chr == 10 || chr == 13 { // \n
				e := i + lineExtra
				for e < len(arr) && (arr[e] == 10 || arr[e] == 13) {
					e++
					lineExtra++
				}
				lineEnds = i
				break
			}
		}

		if lineEnds > -1 {
			// grab the string
			str := string(arr[:lineEnds])

			// move buffer past string
			w.buf = bytes.NewBuffer(arr[lineEnds+lineExtra:])

			// log out
			w.log(w.prefix + str)
		} else {
			break
		}
	}

	return len(p), nil
}
