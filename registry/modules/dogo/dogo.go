package dogo

import (
	"fmt"
	"net"

	"github.com/oliverkofoed/dogo/schema"
	"github.com/oliverkofoed/dogo/snobgob"
)

type DogoState map[string]interface{}

// Manager is the main entry point to this Dogo Module
var Manager = schema.ModuleManager{
	Name:            "dogo",
	ModulePrototype: nil,
	StatePrototype:  make(DogoState),
	GobRegister: func() {
		snobgob.Register(make(map[string][]string))
	},
	GetState: func(query interface{}) (interface{}, error) {
		state := make(DogoState)
		networkInterfaces := make(map[string][]string)
		state["networkinterface"] = networkInterfaces

		ifaces, err := net.Interfaces()
		if err != nil {
			return nil, fmt.Errorf("could not get interfaces. error: %v", err.Error())
		}

		for _, i := range ifaces {
			addrs, err := i.Addrs()
			if err != nil {
				return nil, fmt.Errorf("could not addresses for interfaces %v. error: %v", i.Name, err.Error())
			}

			for _, a := range addrs {
				ip, _, err := net.ParseCIDR(a.String())
				if err != nil {
					return nil, fmt.Errorf("Could not parse %v", a.String())
				}

				arr, found := networkInterfaces[i.Name]
				if !found {
					arr = make([]string, 0, 10)
				}
				networkInterfaces[i.Name] = append(arr, ip.String())
			}
		}

		return state, nil
	},
	CalculateCommands: func(c *schema.CalculateCommandsArgs) error {
		// this module can't change any state. It's purely for reporting purposes.
		return nil
	},
}
