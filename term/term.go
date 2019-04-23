package term

import (
	"bytes"
	"fmt"
	"os"

	"golang.org/x/crypto/ssh/terminal"

	"github.com/mgutz/ansi"
)

const ESC = 27

var IsTerminal = terminal.IsTerminal(int(os.Stdout.Fd()))
var Reset = "\033[0m"
var Red = ansi.ColorCode("red+h")
var White = ansi.ColorCode("white+h")
var Bold = ansi.ColorCode("+b")
var Green = ansi.ColorCode("green+h")
var Yellow = ansi.ColorCode("yellow+h")

func init() {
	if !IsTerminal {
		Reset = ""
		Red = ""
		White = ""
		Green = ""
	}
}

func GetDimensions() (width, height int, err error) {

	return terminal.GetSize(int(os.Stdout.Fd()))
}

func Print(str string) {
	if !buffering {
		print(str)
	} else {
		buf.WriteString(str)
	}
}

func MoveUp(nLines int) {
	Print(fmt.Sprintf("%c[%dA", ESC, nLines))
}

func EraseCurrentLine() {
	Print(fmt.Sprintf("%c[2K\r", ESC))
}

var buf = bytes.Buffer{}
var buffering = false

func StartBuffer() {
	buffering = true
}

func FlushBuffer(printBuffer bool) {
	if printBuffer {
		print(buf.String())
	}
	buf.Reset()
	buffering = false
}
