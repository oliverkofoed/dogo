package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"strings"

	"golang.org/x/crypto/ssh/terminal"

	"github.com/oliverkofoed/dogo/commandtree"
	"github.com/oliverkofoed/dogo/neaterror"
	"github.com/oliverkofoed/dogo/registry/resources/localhost"
	"github.com/oliverkofoed/dogo/schema"
	"github.com/oliverkofoed/dogo/term"
)

func dogoCommand(config *schema.Config, environment *schema.Environment, commandName string, command *schema.Command, commandPackage string, forceTarget string, args []string) {
	setTemplateGlobals(config, environment)

	root := commandtree.NewRootCommand("Run " + commandName)
	err := buildPackageCommands(root, config, environment, commandName, command, commandPackage, forceTarget, func(res *schema.Resource) schema.ServerConnection { return nil }, args)
	if err != nil {
		fmt.Println(neaterror.String("", err, term.IsTerminal))
		return
	}

	if len(root.Children) == 1 {
		c := root.Children[0].(*packageCommand)
		c.execute(true)
	} else {
		r := commandtree.NewRunner(root, 10)
		go r.Run(nil)
		commandtree.ConsoleUI(root)
	}
}

func buildPackageCommands(parent *commandtree.RootCommand, config *schema.Config, environment *schema.Environment, commandName string, command *schema.Command, commandPackage string, forceTarget string, reuseConnection func(res *schema.Resource) schema.ServerConnection, args []string) error {
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
					args:        args,
					local:       command.Local,
					commands:    command.Commands,
					environment: environment,
					resource:    res,
					server:      server,
					connection:  reuseConnection(res),
					tunnels:     make(map[string]*schema.Tunnel),
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

				caption := commandName + " on " + environment.Name + "." + res.Name
				if command.Local {
					caption = commandName + " against " + environment.Name + "." + res.Name
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
		s := &packageCommand{
			args:               args,
			local:              command.Local,
			commands:           command.Commands,
			environment:        environment,
			resource:           nil,
			server:             nil,
			connection:         nil,
			requireRemoteState: false,
			tunnels:            make(map[string]*schema.Tunnel),
		}

		// add command
		parent.Add(commandName, s)
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
	args               []string
	local              bool
	commands           []schema.Template
	resource           *schema.Resource
	environment        *schema.Environment
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
	c.execute(false)
}

func (c *packageCommand) execute(inline bool) {
	fErr := c.Err
	fErrf := c.Errf
	if inline {
		c.LogEvent = func(evt *commandtree.MonitorEvent) {
			if evt.LogEntry != nil {
				if err := evt.LogEntry.Error; err != nil {
					fmt.Println(neaterror.String("", err, term.IsTerminal))
				} else {
					fmt.Println(evt.LogEntry.Message)
				}
			}
		}
	}
	// grab connection
	connection := c.connection
	if connection == nil || (c.local && len(c.tunnels) > 0) {
		if c.resource.Manager.Provision != nil {
			err := c.resource.Manager.Provision(c.resource.ManagerGroup, c.resource.Resource, c)
			if err != nil {
				fErr(err)
				return
			}
		}

		// open connection
		var err error
		connection, err = c.resource.Resource.(schema.ServerResource).OpenConnection()
		if err != nil {
			fErrf("Could not get connection to %v. Err: %v", c.resource.Name, err)
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
			fErr(err)
		}
		if len(expandErrors) > 0 {
			return
		}
	}

	tunnels := make(map[string]*tunnelInfo)
	anyErr := false
	for n, tun := range c.tunnels {
		tunnelHost, err := tun.Host.Render(map[string]interface{}{"self": c.resource.Data})
		if err != nil {
			fErr(err)
			anyErr = true
		} else {
			port, err := connection.StartTunnel(0, tun.Port, tunnelHost, false)
			if err != nil {
				fErrf("Error starting tunnel to %v:%v. Error message: %v", c.resource.Name, tun.Port, err.Error())
				anyErr = true
			}
			tunnels[n] = createTunnelInfo(port)
		}
	}
	if anyErr {
		return
	}

	// render the command string
	renderedCommands := make([]string, 0, len(c.commands))
	for _, cmd := range c.commands {
		commandString, err := cmd.Render(c.getVars(tunnels))
		if err != nil {
			fErr(err)
			//}
			return
		}
		renderedCommands = append(renderedCommands, commandString+" "+strings.Join(c.args, " "))
	}

	if inline {
		fmt.Println(term.Bold + runChar(dashes, 30+len(c.resource.Manager.Name)+len(c.resource.Name)) + term.Reset)
		fmt.Println(term.Bold + "-----[ connected to " + term.Yellow + c.environment.Name + "." + c.resource.Name + term.Bold + " (" + c.resource.Manager.Name + ") ]-----" + term.Reset)
		fmt.Println(term.Bold + runChar(dashes, 30+len(c.resource.Manager.Name)+len(c.resource.Name)) + term.Reset)

		for _, cmd := range renderedCommands {
			//fmt.Println(term.Green + "[ " + cmd + " ]" + term.Reset)
			//fmt.Print("")
			fileDescriptor := int(os.Stdin.Fd())
			if !terminal.IsTerminal(fileDescriptor) {
				fmt.Println(neaterror.String("", fmt.Errorf("Can only run in interative terminal"), term.IsTerminal))
				return
			}

			width, height, err := terminal.GetSize(fileDescriptor)
			if err != nil {
				fmt.Println(neaterror.String("", err, term.IsTerminal))
				return
			}

			if c.local {
				l, err := (&localhost.Localhost{}).OpenConnection()
				if err != nil {
					fmt.Println(neaterror.String("", err, term.IsTerminal))
					return
				}

				err = l.Shell(cmd, os.Stderr, os.Stdout, os.Stdin, width, height)
				if err != nil {
					fmt.Println(neaterror.String("", err, term.IsTerminal))
					return
				}
			} else {
				//fileDescriptor := int(os.Stdin.Fd())
				//if !terminal.IsTerminal(fileDescriptor) {
				//fmt.Println(neaterror.String("", fmt.Errorf("Can only run in interative terminal"), term.IsTerminal))
				//return
				//}

				originalState, err := terminal.MakeRaw(fileDescriptor)
				if err != nil {
					fmt.Println(neaterror.String("", err, term.IsTerminal))
					return
				}

				//width, height, err := terminal.GetSize(fileDescriptor)
				//if err != nil {
				//terminal.Restore(fileDescriptor, originalState)
				//fmt.Println(neaterror.String("", err, term.IsTerminal))
				//return
				//}

				err = connection.Shell(cmd, os.Stderr, os.Stdout, os.Stdin, width, height)
				if err != nil {
					terminal.Restore(fileDescriptor, originalState)
					fmt.Println(neaterror.String("", err, term.IsTerminal))
					return
				}
				terminal.Restore(fileDescriptor, originalState)
			}

		}
	} else if c.local {
		// run command
		for _, cmd := range renderedCommands {
			err := commandtree.OSExec(c.AsCommand(), "", "> ", "/bin/bash", "-c", cmd)
			if err != nil {
				c.Err(err)
				return
			}
		}
	} else {
		// run command on remote system.
		root := commandtree.NewRootCommand("remote command")
		root.Add("Run On "+c.resource.Name, commandtree.NewBashCommands("", "", "> ", renderedCommands...))
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
		port:      port,
		host:      preferredIP,
		loopback:  fmt.Sprintf("127.0.0.1:%v", port),
		preferred: fmt.Sprintf("%v:%v", preferredIP, port),
	}
}

type tunnelInfo struct {
	port      int
	host      string
	loopback  string
	preferred string
}

func (t *tunnelInfo) String() string {
	return t.preferred
}
