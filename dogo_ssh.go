package main

import (
	"fmt"
	"os"

	"golang.org/x/crypto/ssh/terminal"

	"github.com/oliverkofoed/dogo/neaterror"
	"github.com/oliverkofoed/dogo/schema"
	"github.com/oliverkofoed/dogo/term"
)

func dogoSSH(config *schema.Config, environment *schema.Environment, target string) error {
	setTemplateGlobals(config, environment)

	// find server
	var targetServer schema.ServerResource
	var targetResource *schema.Resource

	for name, res := range environment.Resources {
		if name == target {
			if server, ok := res.Resource.(schema.ServerResource); ok {
				targetServer = server
				targetResource = res
			} else {
				return fmt.Errorf("%v is not a server. It's a %v", name, res.Manager.Name)
			}
			break
		}
	}
	if targetServer == nil {
		return fmt.Errorf("Unknown server: %v", target)
	}

	// provision if required.
	if targetResource.Manager.Provision != nil {
		fmt.Printf("Provisioning %v (%v)\n", targetResource.Name, targetResource.Manager.Name)
		err := targetResource.Manager.Provision(targetResource.ManagerGroup, targetResource.Resource, &schema.PrefixLogger{Output: &schema.ConsoleLogger{}, Prefix: " - "})
		if err != nil {
			return err
		}
	}

	// establish connection
	fmt.Printf("Establishing connection to %v (%v)\n", targetResource.Name, targetResource.Manager.Name)
	connection, err := targetServer.OpenConnection()
	if err != nil {
		return err
	}

	// Remove "connecting..." line and print banner
	if term.IsTerminal {
		term.MoveUp(1)
		term.EraseCurrentLine()
	}
	fmt.Println(term.White + runChar(dashes, 30+len(targetResource.Manager.Name)+len(targetResource.Name)) + term.Reset)
	fmt.Println(term.White + "-----[ connected to " + term.Yellow + environment.Name + "." + targetResource.Name + term.White + " (" + targetResource.Manager.Name + ") ]-----" + term.Reset)
	fmt.Println(term.White + runChar(dashes, 30+len(targetResource.Manager.Name)+len(targetResource.Name)) + term.Reset)
	fmt.Println()

	fileDescriptor := int(os.Stdin.Fd())
	if !terminal.IsTerminal(fileDescriptor) {
		fmt.Println(neaterror.String("", fmt.Errorf("Can only run in interative terminal"), term.IsTerminal))
	}

	originalState, err := terminal.MakeRaw(fileDescriptor)
	if err != nil {
		return err
	}
	defer terminal.Restore(fileDescriptor, originalState)

	width, height, err := terminal.GetSize(fileDescriptor)
	if err != nil {
		return err
	}

	// run a shell
	return connection.Shell("", os.Stderr, os.Stdout, os.Stdin, width, height)
}
