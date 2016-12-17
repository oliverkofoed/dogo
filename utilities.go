package main

import (
	"bytes"
	"fmt"

	"github.com/oliverkofoed/dogo/neaterror"
	"github.com/oliverkofoed/dogo/term"
)

func printErrors(errs []error) {
	if errs != nil {
		first := true
		for _, err := range errs {
			if !first {
				fmt.Println()
			}
			fmt.Println(neaterror.String("Config error: ", err, term.IsTerminal))
			first = false
		}
		return
	}
}

var spaces = []rune("                                                                            ")
var dashes = []rune("----------------------------------------------------------------------------")

func runChar(input []rune, length int) string {
	if length < 0 {
		return ""
	}
	if length < len(input) {
		return string(input[:length])
	}

	b := bytes.NewBuffer(nil)
	for i := 0; i != length; i++ {
		b.WriteString(string(input[0]))
	}
	return b.String()
}
