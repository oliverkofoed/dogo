package neaterror

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/oliverkofoed/dogo/term"
)

type Error struct {
	Message string
	Prefix  string
	Data    map[string]interface{}
}

func (e Error) Error() string {
	return String("", e, false)
}

func (e Error) String() string {
	return String("", e, false)
}

func New(data map[string]interface{}, errorFormat string, args ...interface{}) Error {
	return Error{
		Message: fmt.Sprintf(errorFormat, args...),
		Data:    data,
	}
}

func Enrich(e error, extra map[string]interface{}) Error {
	if n, ok := e.(Error); ok {
		for k, v := range extra {
			n.Data[k] = v
		}
		return n
	}
	return Error{Message: e.Error(), Data: extra}
}

func Println(prefix string, terminalColors bool, data map[string]interface{}, errorFormat string, args ...interface{}) {
	fmt.Println(String(prefix, New(data, errorFormat, args...), terminalColors))
}

func String(prefix string, err error, terminalColors bool) string {
	prefixColor := ""
	lineColor := ""
	keyColor := ""
	valueColor := ""
	resetColor := ""
	errorColor := ""
	if terminalColors {
		prefixColor = term.Reset
		lineColor = term.Yellow
		keyColor = term.Bold
		valueColor = term.Reset
		resetColor = term.Reset
		errorColor = term.Red
	}

	b := bytes.NewBuffer(nil)

	b.WriteString(prefixColor)
	if s, ok := err.(Error); ok {
		b.WriteString(s.Prefix)
		b.WriteString(prefix)
		b.WriteString(errorColor)
		b.WriteString(s.Message)
		if s.Data != nil {
			b.WriteString("\n")
			b.WriteString(stringer2(s.Data, spaces(len(s.Prefix)+len(prefix)), true, lineColor, keyColor, valueColor, resetColor))
		}
		return b.String()
	} else {
		b.WriteString(prefix)
		b.WriteString(errorColor)
		b.WriteString(err.Error())
		b.WriteString(resetColor)
		return b.String()
	}
}

func stringer2(v interface{}, prefix string, root bool, lineColor, keyColor, valueColor, resetColor string) string {
	b := bytes.NewBuffer(nil)
	if m, ok := v.(map[string]interface{}); ok {
		maxNameLength := 0
		for k := range m {
			l := len(k)
			if len(k) > 0 && k[0] == "!"[0] {
				l--
			}

			if l > maxNameLength {
				maxNameLength = l
			}
		}
		for i, k := range sortKeys(m) {
			v := m[k]
			last := i == len(m)-1
			b.WriteString(lineColor)
			if root || i != 0 {
				b.WriteString(prefix)
			}
			if root && len(m) == 1 {
				b.WriteString("└")
			} else if root && len(m) > 1 && i == 0 {
				b.WriteString("├")
			} else if len(m) == 1 {
				b.WriteString("─")
			} else if last {
				b.WriteString("└")
			} else if i == 0 {
				b.WriteString("┬")
			} else {
				b.WriteString("├")
			}
			kn := k
			if len(kn) > 0 && kn[0] == "!"[0] {
				kn = kn[1:]
			}
			for i := 0; i < maxNameLength-len(kn)+1; i++ {
				b.WriteString("─")
			}
			b.WriteString(" ")
			b.WriteString(keyColor)
			b.WriteString(kn)
			b.WriteString(": ")
			b.WriteString(resetColor)
			prefixLine := "│"
			if last {
				prefixLine = " "
			}
			b.WriteString(stringer2(v, prefix+prefixLine+spaces(maxNameLength+4), false, lineColor, keyColor, valueColor, resetColor))
			if !last {
				b.WriteString("\n")
			}
		}
	} else if arr, ok := v.([]interface{}); ok {
		for i, v := range arr {
			last := i == len(arr)-1
			b.WriteString(lineColor)
			if root || i != 0 {
				b.WriteString(prefix)
			}
			if len(arr) == 1 {
				b.WriteString("─")
			} else if i == 0 {
				b.WriteString("┬")
			} else if last {
				b.WriteString("└")
			} else {
				b.WriteString("├")
			}
			for x := 0; x < len(arr)/10-i/10-1; x++ {
				b.WriteString("─")
			}
			b.WriteString(keyColor)
			b.WriteString(fmt.Sprintf("[%v]", i))
			b.WriteString(": ")
			b.WriteString(resetColor)
			prefixLine := "│"
			if last {
				prefixLine = " "
			}
			b.WriteString(stringer2(v, prefix+prefixLine+spaces(len(arr)/10+4), false, lineColor, keyColor, valueColor, resetColor))
			if !last {
				b.WriteString("\n")
			}
		}
	} else {
		b.WriteString(valueColor)
		b.WriteString(fmt.Sprintf("%v", v))
		b.WriteString(resetColor)
	}
	return b.String()
}

func sortKeys(m map[string]interface{}) []string {
	arr := make([]string, 0, len(m))
	for k := range m {
		arr = append(arr, k)
	}
	sort.Strings(arr)
	return arr
}

var spacearr = []rune("                                                                            ")

func spaces(length int) string {
	if length < len(spacearr) {
		return string(spacearr[:length])
	}

	b := bytes.NewBuffer(nil)
	for i := 0; i != length; i++ {
		b.WriteString(" ")
	}
	return b.String()
}
