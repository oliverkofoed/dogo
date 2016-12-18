package main

import (
	"fmt"
	"os"

	"encoding/json"

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
	case "getstate":
		for name, manager := range registry.ModuleManagers {
			fmt.Println("Module: " + name)
			if manager.CalculateGetStateQuery == nil {
				state, err := manager.GetState(nil)
				if err != nil {
					fmt.Println(" - error: " + err.Error())
					continue
				}
				j, err := json.Marshal(state)
				if err != nil {
					fmt.Println(" - error: got state successfully, but could not turn it into json: " + err.Error())
					continue
				}

				fmt.Println(" - state: " + string(j))
			} else {
				fmt.Println(" - skipping getting state since a query object is required.")
			}
		}
	}
}
