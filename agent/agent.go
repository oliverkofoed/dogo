package main

import (
	"fmt"
	"os"

	"github.com/oliverkofoed/dogo/commandtree"
	"github.com/oliverkofoed/dogo/registry"
	"github.com/oliverkofoed/dogo/version"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Dogo Agent Version:", version.Version)
		return
	}

	registry.GobRegister()

	switch os.Args[1] {
	case "exec":
		err := commandtree.StreamReceive(os.Stdin, os.Stdout)
		if err != nil {
			panic(err)
		}
		return
	}
}
