package main

import (
	"fmt"
	"io"
	"net"

	"github.com/oliverkofoed/dogo/commandtree"
	"github.com/oliverkofoed/dogo/neaterror"
	"github.com/oliverkofoed/dogo/schema"
	"github.com/oliverkofoed/dogo/term"
)

func dogoCommand(config *schema.Config, environment *schema.Environment, commandName string, command *schema.Command, commandPackage string, forceTarget string) {
	setTemplateGlobals(config, environment)

	root := commandtree.NewRootCommand("Run " + commandName)
	err := buildPackageCommands(root, config, environment, commandName, command, commandPackage, forceTarget, func(res *schema.Resource) schema.ServerConnection { return nil })
	if err != nil {
		fmt.Println(neaterror.String("", err, term.IsTerminal))
		return
	}

	if len(root.Children) == 1 {
		c := root.Children[0]
		go func() {
			c.Execute()
			c.AsCommand().State = commandtree.CommandStateCompleted
		}()
		commandtree.SingleCommandUI(c.AsCommand())
	} else {
		r := commandtree.NewRunner(root, 5)
		go r.Run(nil)
		commandtree.ConsoleUI(root)
	}
}

func buildPackageCommands(parent *commandtree.RootCommand, config *schema.Config, environment *schema.Environment, commandName string, command *schema.Command, commandPackage string, forceTarget string, reuseConnection func(res *schema.Resource) schema.ServerConnection) error {
	// find the target string
	target, err := command.Target.Render(nil)
	if err != nil {
		if n, ok := err.(neaterror.Error); ok {
			n.Prefix = "Bad target template: "
			return n
		}
		return err
	}
	if forceTarget != "" {
		target = forceTarget
	}

	// calculate target servers from target string (either "first" or "*" or "servername")
	packResources, found := environment.ResourcesByPackage[commandPackage]
	discardReasons := make(map[string]interface{})
	addedAtlestOne := false
	if found && len(packResources) > 0 {
		for _, res := range packResources {
			if server, ok := res.Resource.(schema.ServerResource); ok {
				s := &packageCommand{
					local:      command.Local,
					commands:   command.Commands,
					resource:   res,
					server:     server,
					connection: reuseConnection(res),
					tunnels:    make(map[string]*schema.Tunnel),
				}

				// check that this server has all packages required by the given tunnels.
				if command.Local {
					for _, tunnelName := range command.Tunnels {
						for packageName := range res.Packages {
							if pack, found := config.Packages[packageName]; found {
								if tun, found := pack.Tunnels[tunnelName]; found {
									s.tunnels[tunnelName] = tun
									break
								}
							}
						}
					}
					if len(s.tunnels) != len(command.Tunnels) {
						t := make([]string, 0, len(command.Tunnels))
						for n := range s.tunnels {
							t = append(t, n)
						}
						discardReasons[res.Name] = fmt.Sprintf("does not have all the required tunnels (%v). Only has (%v).", command.Tunnels, t)
						continue
					}
				}

				// try to render all templates, and if any of them fail that
				// template probably requires remote state, so mark for remote
				// state collection.
				pretendTunnels := make(map[string]*tunnelInfo)
				i := 0
				for n := range s.tunnels {
					pretendTunnels[n] = createTunnelInfo(9872 + i)
					i++
				}
				for _, cmd := range s.commands {
					if _, err := cmd.Render(s.getVars(pretendTunnels)); err != nil {
						s.requireRemoteState = true
						break
					}
				}
				s.requireRemoteState = s.requireRemoteState || len(expandResourceTemplates(s.resource, config)) > 0

				caption := commandName + " on " + res.Name
				if command.Local {
					caption = commandName + " against " + res.Name
				}
				switch target {
				case "":
					parent.Add(caption, s)
					addedAtlestOne = true
					break
				case "*":
					parent.Add(caption, s)
					addedAtlestOne = true
				case "first":
					if !addedAtlestOne {
						parent.Add(caption, s)
						addedAtlestOne = true
					}
				default:
					if res.Name == target {
						parent.Add(caption, s)
						addedAtlestOne = true
					} else {
						discardReasons[res.Name] = fmt.Sprintf("does not have the name: %v", target)
					}
				}
			}
		}
	}

	// is this a command that just will be run locally?
	if !addedAtlestOne && command.Local && len(command.Tunnels) == 0 {
		// render the commands array
		arr := make([]string, 0, len(command.Commands))
		for _, cmd := range command.Commands {
			commandString, err := cmd.Render(nil)
			if err != nil {
				return err
			}
			arr = append(arr, commandString)
		}

		// add command
		parent.Add(commandName, commandtree.NewBashCommands("", "", "", arr...))
		addedAtlestOne = true
	}

	// error out if we don't have any servers to target.
	if !addedAtlestOne {
		return neaterror.New(discardReasons, "Could not find any servers to run the command '%v' on.", commandName)
	}

	return nil
}

type packageCommand struct {
	commandtree.Command
	local              bool
	commands           []schema.Template
	resource           *schema.Resource
	server             schema.ServerResource
	connection         schema.ServerConnection
	requireRemoteState bool
	tunnels            map[string]*schema.Tunnel // tunnelname => tunnel
}

func (c *packageCommand) getVars(tunnels map[string]*tunnelInfo) map[string]interface{} {
	vars := make(map[string]interface{})
	vars["tunnel"] = tunnels
	vars["self"] = c.resource.Data
	return vars
}

func (c *packageCommand) Execute() {
	// grab connection
	connection := c.connection
	if connection == nil || (c.local && len(c.tunnels) > 0) {
		if c.resource.Manager.Provision != nil {
			err := c.resource.Manager.Provision(c.resource.ManagerGroup, c.resource.Resource, c)
			if err != nil {
				c.Err(err)
				return
			}
		}

		// open connection
		var err error
		connection, err = c.resource.Resource.(schema.ServerResource).OpenConnection()
		if err != nil {
			c.Errf("Could not get connection to %v. Err: %v", c.resource.Name, err)
			return
		}
		defer connection.Close()

	}

	// get remote state if required
	if c.requireRemoteState {
		_, _, success := getState(c.resource, connection, false, c, c)
		if !success {
			return
		}

		// expand templates
		expandErrors := expandResourceTemplates(c.resource, config)
		for _, err := range expandErrors {
			c.Err(err)
		}
		if len(expandErrors) > 0 {
			return
		}
	}

	tunnels := make(map[string]*tunnelInfo)
	if c.local {
		// start tunnels.
		anyErr := false
		for n, tun := range c.tunnels {
			port, err := connection.StartTunnel(0, tun.Port, false)
			if err != nil {
				c.Errf("Error starting tunnel to %v:%v. Error message: %v", c.resource.Name, tun.Port, err.Error())
				anyErr = true
			}
			tunnels[n] = createTunnelInfo(port)
		}
		if anyErr {
			return
		}

		// render the command string
		for _, cmd := range c.commands {
			commandString, err := cmd.Render(c.getVars(tunnels))
			if err != nil {
				c.Err(err)
				return
			}

			// run command
			err = commandtree.OSExec(c.AsCommand(), "", "> ", "/bin/bash", "-c", commandString)
			if err != nil {
				c.Err(err)
				return
			}
		}
	} else {
		// render the commands array
		arr := make([]string, 0, len(c.commands))
		for _, cmd := range c.commands {
			commandString, err := cmd.Render(nil)
			if err != nil {
				c.Err(err)
				return
			}
			arr = append(arr, commandString)
		}

		// run command on remote system.
		root := commandtree.NewRootCommand("remote command")
		root.Add("Run On "+c.resource.Name, commandtree.NewBashCommands("", "", "> ", arr...))
		err := connection.ExecutePipeCommand(schema.AgentPath+" exec", func(reader io.Reader, errorReader io.Reader, writer io.Writer) error {
			return commandtree.StreamCall(root, c, 1, reader, errorReader, writer, func(s string) { c.Logf(s) })
		})
		if err != nil {
			c.Err(err)
		}
	}
}

var preferredIP = "127.0.0.1"

func createTunnelInfo(port int) *tunnelInfo {
	if preferredIP == "127.0.0.1" {
		if ifaces, err := net.Interfaces(); err == nil {
			for _, i := range ifaces {
				if addrs, err := i.Addrs(); err == nil {
					for _, addr := range addrs {
						switch v := addr.(type) {
						case *net.IPNet:
							if ipv4 := v.IP.To4(); ipv4 != nil {
								if preferredIP == "127.0.0.1" && (i.Flags&net.FlagLoopback == 0) {
									preferredIP = ipv4.String()
								}
							}
						}
					}
				}
			}
		}
	}

	return &tunnelInfo{
		Port:      port,
		Loopback:  fmt.Sprintf("127.0.0.1:%v", port),
		preferred: fmt.Sprintf("%v:%v", preferredIP, port),
	}
}

type tunnelInfo struct {
	Port      int
	Loopback  string
	preferred string
}

func (t *tunnelInfo) String() string {
	return t.preferred
}
