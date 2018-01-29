package main

import (
	"bytes"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/oliverkofoed/dogo/commandtree"
	"github.com/oliverkofoed/dogo/neaterror"
	"github.com/oliverkofoed/dogo/registry"
	"github.com/oliverkofoed/dogo/registry/modules/dogo"
	"github.com/oliverkofoed/dogo/schema"
	"github.com/oliverkofoed/dogo/version"
)

type deployStep int

const (
	deployStepGatherState              deployStep = iota // 0
	deployStepExpandTemplates                            // 1
	deployStepCalculateCommands                          // 2
	deployStepBeforeDeploymentCommands                   // 3
	deployStepLocalCommands                              // 4
	deployStepRemoteCommands                             // 5
	deployStepAfterDeploymentCommands                    // 6
	deployStepDecommission                               // 7
	deployStepDone                                       // 8
)

func sortKeys(m map[string]*schema.Resource) []string {
	arr := make([]string, 0, len(m))
	for k := range m {
		arr = append(arr, k)
	}
	sort.Strings(arr)
	return arr
}

func dogoDeploy(config *schema.Config, environment *schema.Environment, allowDecommission bool) {
	// build template globals.
	setTemplateGlobals(config, environment)

	// create deploy commands
	deployCommands := make(map[string]*deployCommand)
	deployTask := commandtree.NewRootCommand("Deploying " + environment.Name)
	for _, name := range sortKeys(environment.Resources) {
		res := environment.Resources[name]
		cmd := &deployCommand{
			res:         res,
			name:        name,
			step:        deployStepGatherState,
			remoteState: &schema.ServerState{},
			config:      config,
			environment: environment,
		}
		deployCommands[name] = cmd
		deployTask.Add(environment.Name+"."+name, cmd)
	}

	// Run!
	r := commandtree.NewRunner(deployTask, 5)

	go func() {
		calcHooksCommand := &calculateDeploymentHooksCommand{
			environment: environment,
			config:      config,
			beforeDeploymentCommands: commandtree.NewRootCommand("before_deployment"),
			afterDeploymentCommands:  commandtree.NewRootCommand("before_deployment"),
			reuseConnection: func(res *schema.Resource) schema.ServerConnection {
				for _, d := range deployCommands {
					if d.res.Name == res.Name {
						return d.connection
					}
				}
				return nil
			},
		}
		findUnusedServersCommand := &findUnusedServersCommand{
			environment:       environment,
			allowDecommission: allowDecommission,
		}

		step := deployStepGatherState
		for {
			// pre-run steps
			switch step {
			case deployStepCalculateCommands:
				if len(environment.DeploymentHooks) > 0 {
					deployTask.Add("Calculate deployment hooks", calcHooksCommand)
				}
			case deployStepBeforeDeploymentCommands:
				for _, cmd := range calcHooksCommand.beforeDeploymentCommands.Children {
					deployTask.Add("before_deployment: "+cmd.AsCommand().Caption, cmd)
				}
			case deployStepAfterDeploymentCommands:
				for _, cmd := range calcHooksCommand.afterDeploymentCommands.Children {
					deployTask.Add("after_deployment: "+cmd.AsCommand().Caption, cmd)
				}
			case deployStepDecommission:
				deployTask.Add("Check for unused servers", findUnusedServersCommand)
			}

			// set state
			for _, t := range deployCommands {
				t.step = step
				if step == deployStepDone {
					t.State = commandtree.CommandStateCompleted
				} else {
					t.State = commandtree.CommandStateReady
				}
			}

			// do a run.
			noErrors := r.Run(nil)
			if !noErrors || step == deployStepDone {
				for _, t := range deployCommands { // to stop statusprinter
					t.State = commandtree.CommandStateCompleted
				}
				break
			}

			// move forward a step
			step = step + 1
		}

		// cleanup after ourselves
		closeConnections(deployCommands)
	}()

	// start a console monitor
	commandtree.ConsoleUI(deployTask)
}

type calculateDeploymentHooksCommand struct {
	commandtree.Command
	config                   *schema.Config
	environment              *schema.Environment
	reuseConnection          func(res *schema.Resource) schema.ServerConnection
	beforeDeploymentCommands *commandtree.RootCommand
	afterDeploymentCommands  *commandtree.RootCommand
}

func (c *calculateDeploymentHooksCommand) Execute() {
	for _, h := range c.environment.DeploymentHooks {
		var parent *commandtree.RootCommand
		if h.RunBeforeDeployment {
			parent = c.beforeDeploymentCommands
		} else {
			parent = c.afterDeploymentCommands
		}
		err := buildPackageCommands(parent, c.config, c.environment, h.CommandName, h.Command, h.CommandPackage, "", c.reuseConnection, make([]string, 0))

		if err != nil {
			c.Err(err)
		}
	}
}

func closeConnections(commands map[string]*deployCommand) {
	for _, cmd := range commands {
		if cmd.connection != nil {
			cmd.connection.Close()
		}
	}
}

type deployCommand struct {
	commandtree.Command
	name           string
	res            *schema.Resource
	connection     schema.ServerConnection
	step           deployStep
	remoteState    *schema.ServerState
	config         *schema.Config
	environment    *schema.Environment
	localCommands  *commandtree.RootCommand
	remoteCommands *commandtree.RootCommand
	requireSudo    bool
}

func (c *deployCommand) Execute() {
	switch c.step {
	case deployStepGatherState:
		c.stepGatherState()
	case deployStepExpandTemplates:
		c.stepExpandTemplates()
	case deployStepCalculateCommands:
		c.stepCalculateCommands()
	case deployStepLocalCommands:
		c.stepLocalCommands()
	case deployStepRemoteCommands:
		c.stepRemoteCommands()
	}
	c.State = commandtree.CommandStatePaused
}

func (c *deployCommand) stepGatherState() {
	// 1. Provision (if needed)
	if c.res.Manager.Provision != nil {
		c.Logf("Provisioning %v (%v)", c.name, c.res.Manager.Name)
		err := c.res.Manager.Provision(c.res.ManagerGroup, c.res.Resource, c)
		if err != nil {
			if n, ok := err.(neaterror.Error); ok {
				n.Prefix = fmt.Sprintf("Could not provision %v (%v): ", c.name, c.res.Manager.Name)
				c.Err(n)
			} else {
				c.Errf("Could not provision %v (%v): %v", c.name, c.res.Manager.Name, err.Error())
			}
			return
		}
	}

	// 2. Connect.
	if server, ok := c.res.Resource.(schema.ServerResource); ok {
		c.Logf("Establishing connection to %v (%v)", c.name, c.res.Manager.Name)
		connection, err := server.OpenConnection()
		if err != nil {
			c.Err(err)
			return
		}
		c.connection = connection
	}

	// 3. Get state
	success := false
	c.remoteState, c.requireSudo, success = getState(c.res, c.connection, c.requireSudo, c, c)
	if !success {
		return
	}

	// wait for others
	c.Logf("Waiting for state to be gathered from other servers")
}

func (c *deployCommand) stepExpandTemplates() {
	for _, err := range expandResourceTemplates(c.res, c.config) {
		c.Err(err)
	}
}

func (c *deployCommand) stepCalculateCommands() {
	// calculate commands needed to modify server state to correctness
	c.Logf("Calculating commands to modify server to target state.")
	start := time.Now()

	// create reusable args object
	c.remoteCommands = commandtree.NewRootCommand("Remote Commands")
	c.localCommands = commandtree.NewRootCommand("Local Commands")
	args := schema.CalculateCommandsArgs{
		LocalCommands:    c.localCommands,
		RemoteCommands:   c.remoteCommands,
		RemoteConnection: c.connection,
		Environment:      c.environment,
		Config:           c.config,
	}

	// calculate changes for each module
	anyError := false
	for _, m := range registry.ModuleManagers {
		prefix := " - " + m.Name + ": "
		args.Logf = func(format string, ax ...interface{}) { c.Logf(prefix+format, ax...) }
		args.Err = func(err error) {
			if n, ok := err.(neaterror.Error); ok {
				n.Prefix = prefix
				c.Err(n)
			} else {
				c.Err(neaterror.Error{
					Prefix:  prefix,
					Message: err.Error(),
				})
			}
		}
		args.Errf = func(format string, ax ...interface{}) { c.Errf(prefix+format, ax...) }
		if m.CalculateCommands == nil {
			panic(fmt.Sprintf("%v.CalculateCommands is nil. Should be a func.", m.Name))
		}

		args.State = c.remoteState.Modules[m.Name]
		args.Modules = c.res.Modules[m.Name]
		err := m.CalculateCommands(&args)
		if err != nil {
			args.Err(err)
			anyError = true
		}
	}
	if anyError {
		return
	}

	if time.Since(start) > time.Second {
		c.Logf(" - warning: it took %v", time.Since(start))
	}

	// bail if nothing to do.
	if len(c.remoteCommands.Children) == 0 && len(args.LocalCommands.Children) == 0 {
		c.Logf("Remote system has correct state. No changes required.")
	}
}

func (c *deployCommand) stepLocalCommands() {
	if c.localCommands != nil && len(c.localCommands.Children) > 0 {
		c.Logf("Executing local commands.")
		for _, child := range c.localCommands.Children {
			c.Add(child.AsCommand().Caption, child)
		}
	}
}

func (c *deployCommand) stepRemoteCommands() {
	if len(c.remoteCommands.Children) > 0 {
		c.Logf("Executing remote commands.")
		var err error
		c.requireSudo, err = sudoRetry(c.requireSudo, func(sudo bool, cmdPrefix string) error {
			return c.connection.ExecutePipeCommand(cmdPrefix+schema.AgentPath+" exec", func(reader io.Reader, errorReader io.Reader, writer io.Writer) error {
				err := commandtree.StreamCall(c.remoteCommands, c, 5, reader, errorReader, writer, func(s string) { c.Logf(s) })
				return err
			})
		})

		if err != nil {
			c.Errf(err.Error())
			return
		}
	}
	return
}

func getState(resource *schema.Resource, connection schema.ServerConnection, useSudo bool, owner commandtree.CommandNode, l schema.Logger) (*schema.ServerState, bool, bool) {
	// build the state query
	getStateQuery := make(map[string]interface{})
	for name, manager := range registry.ModuleManagers {
		if manager.CalculateGetStateQuery == nil {
			getStateQuery[name] = registry.DefaultStateQuery{}
		} else {
			prefix := " - " + name + ": "
			args := &schema.CalculateGetStateQueryArgs{}
			args.Logf = func(format string, ax ...interface{}) { l.Logf(prefix+format, ax...) }
			args.Err = func(err error) {
				if n, ok := err.(neaterror.Error); ok {
					n.Prefix = prefix
					l.Err(n)
				} else {
					l.Err(neaterror.Error{
						Prefix:  prefix,
						Message: err.Error(),
					})
				}
			}
			args.Errf = func(format string, ax ...interface{}) { l.Errf(prefix+format, ax...) }
			args.Modules = resource.Modules[name]

			query, err := manager.CalculateGetStateQuery(args)
			if err != nil {
				l.Errf("Error calculating state query for %v: %v", name, err)
				return nil, useSudo, false
			}

			if query != nil {
				getStateQuery[name] = query
			}
		}
	}

	updateAgent := false
	agentExists := true
	l.Logf("Getting machine state with dogoagent (%v)", schema.AgentPath)
	remoteState, useSudo, err := executeGetState(useSudo, getStateQuery, "Get state from existing agent", connection, owner)
	if err != nil {
		agentExists = !isCommandNotFound(err)
		updateAgent = true
	} else if remoteState.Version != version.Version {
		l.Logf(" - server running outdated dogoagent version %v. Current version is %v. Will update.", remoteState.Version, version.Version)
		updateAgent = true
	} else if remoteState.OS != "darwin" && remoteState.UID != 0 {
		useSudo = true
		serverState, _, err := executeGetState(useSudo, getStateQuery, "Get state from existing agent", connection, owner)
		if err != nil {
			l.Err(err)
			return nil, useSudo, false
		}
		remoteState = serverState
	}

	if updateAgent {
		l.Logf("Uploading new agent")

		// get os name
		os := ""
		if remoteState != nil {
			os = remoteState.OS
		}
		if os == "" {
			l.Logf(" - checking OS (uname)")
			os = "linux"
			uname, err := connection.ExecuteCommand("uname")
			if err == nil {
				if strings.Contains(uname, "Darwin") {
					os = "darwin"
				}
			}
			l.Logf("   it's %v. (%v)", os, strings.Replace(uname, "\n", "", 1))
		}

		// delete preexisting agent file
		if agentExists {
			l.Logf(" - deleting preexisting dogoagent (%v)", schema.AgentPath)
			useSudo, err = sudoRetry(useSudo, func(sudo bool, cmdPrefix string) error {
				_, err := connection.ExecuteCommand(cmdPrefix + "rm " + schema.AgentPath)
				return err
			})
			if err != nil {
				l.Errf("Error deleting %v: %v. Giving Up!", schema.AgentPath, err)
				return nil, useSudo, false
			}
		}

		// upload agent
		l.Logf(" - uploading agent version: %v", version.Version)
		agentBytes, err := Asset("agent/.build/agent." + os)
		if err != nil {
			panic(err)
		}
		useSudo, err = sudoRetry(useSudo, func(sudo bool, cmdPrefix string) error {
			return connection.WriteFile(schema.AgentPath, 755, int64(len(agentBytes)), bytes.NewReader(agentBytes), sudo, l.SetProgress)
		})
		l.SetProgress(0)
		if err != nil {
			l.Errf("Could not upload agent to %v. Err:%v", schema.AgentPath, err.Error())
			return nil, useSudo, false
		}

		// refresh the DogoAgent state
		l.Logf(" - getting machine state (again) with %v", schema.AgentPath)
		remoteState, useSudo, err = executeGetState(useSudo, getStateQuery, "Get state from newly installed agent.", connection, owner)
		if err != nil {
			l.Err(err)
			return nil, useSudo, false
		} else if remoteState.Version != version.Version {
			l.Errf("Got bad version from dogoagent (%v). Expected %v. Apparently updating didn't work. Giving up", remoteState.Version, version.Version)
			return nil, useSudo, false
		}
	}

	// Check for errors in result
	anyErrors := false
	for m, s := range remoteState.Modules {
		if err, ok := s.(error); ok {
			n, ok := err.(neaterror.Error)
			if !ok {
				n = neaterror.New(nil, err.Error())
			}
			n.Prefix = fmt.Sprintf("Error getting %v state: ", m) + n.Prefix
			l.Err(n)
			anyErrors = true
		}
	}
	if anyErrors {
		return nil, useSudo, false
	}

	// copy special dogo args.
	for k, v := range remoteState.Modules["dogo"].(dogo.DogoState) {
		resource.Data[k] = reflect.ValueOf(v).Interface()
	}

	return remoteState, useSudo, true
}

func executeGetState(useSudo bool, getStateQuery map[string]interface{}, caption string, connection schema.ServerConnection, owner commandtree.CommandNode) (*schema.ServerState, bool, error) {
	root := commandtree.NewRootCommand("Get Agent State")
	root.Add(caption, &registry.GetStateCommand{
		Query: getStateQuery,
	})

	cmd := owner.AsCommand()

	useSudo, err := sudoRetry(useSudo, func(sudo bool, cmdPrefix string) error {
		return connection.ExecutePipeCommand(cmdPrefix+schema.AgentPath+" exec", func(reader io.Reader, errorReader io.Reader, writer io.Writer) error {
			start := time.Now()
			err := commandtree.StreamCall(root, owner, 5, reader, errorReader, writer, func(s string) { cmd.Logf(s) })
			duration := time.Since(start)
			if duration > time.Second {
				cmd.Logf(" - warning: it took %v", duration)
			}
			return err
		})
	})
	if err != nil {
		if isCommandNotFound(err) {
			cmd.Logf(" - could not find dogoagent (%v)", schema.AgentPath)
		} else {
			cmd.Errf("Error running %v: %v", schema.AgentPath, err)
		}
		return nil, useSudo, err
	}

	// Dig out state from command, and remove empty Get state children (for our own sanity)
	var serverState *schema.ServerState
	newChildren := make([]commandtree.CommandNode, 0, len(owner.AsCommand().Children))
	for _, child := range owner.AsCommand().Children {
		result := child.AsCommand().GetResult()
		if result != nil {
			if s, ok := result.(schema.ServerState); ok {
				serverState = &s
				if len(child.AsCommand().LogArray) == 0 {
					continue
				}
			}
		}
		newChildren = append(newChildren, child)
	}
	owner.AsCommand().Children = newChildren
	if serverState == nil {
		return nil, useSudo, fmt.Errorf("did not get result from dogoagent")
	}

	return serverState, useSudo, nil
}

func isCommandNotFound(err error) bool {
	low := strings.ToLower(err.Error())
	return strings.Contains(low, "command not found") ||
		strings.Contains(low, "127") ||
		strings.Contains(low, "no such file or directory")
}

func isPermissionDenied(err error) bool {
	low := strings.ToLower(err.Error())
	return strings.Contains(low, "permission denied")
}

func sudoRetry(useSudo bool, f func(sudo bool, cmdPrefix string) error) (bool, error) {
	// first try with current settings
	cmdPrefix := ""
	if useSudo {
		cmdPrefix = "sudo -n "
	}
	err := f(useSudo, cmdPrefix)

	// if we're not already requring sudo, and this was a permission
	// deined error, try with sudo, and if it works, start requring sudo.
	if err != nil && isPermissionDenied(err) && !useSudo {
		err = f(true, "sudo -n ")
		if err == nil {
			useSudo = true
		}
	}

	return useSudo, err
}

func expandResourceTemplates(resource *schema.Resource, config *schema.Config) []error {
	var errors []error

	vars := map[string]interface{}{"self": resource.Data}
	for k, v := range resource.Data {
		if str, ok := v.(string); ok {
			// check if it's a template, by looking for '{{'
			if strings.Contains(str, "{{") {
				t, err := config.TemplateSource.NewTemplate(k, str, vars)
				if err != nil {
					errors = append(errors, err)
					continue
				}

				output, err := t.Render(nil)
				if err != nil {
					errors = append(errors, err)
				} else {
					resource.Data[k] = output
				}
			}
		}
	}

	return errors
}

type findUnusedServersCommand struct {
	commandtree.Command
	environment       *schema.Environment
	allowDecommission bool
}

func (c *findUnusedServersCommand) Execute() {
	printInstruction := false
	used := make(map[string]map[interface{}][]string) // managername => managergroup => servername[]

	for manager, groups := range c.environment.ManagerGroups {
		m, found := used[manager]
		if !found {
			m = make(map[interface{}][]string)
			used[manager] = m
		}

		for _, g := range groups {
			m[g] = make([]string, 0)
		}
	}

	for _, res := range c.environment.Resources {
		m, found := used[res.Manager.Name]
		if !found {
			m = make(map[interface{}][]string)
			used[res.Manager.Name] = m
		}

		arr, found := m[res.ManagerGroup]
		if !found {
			arr = make([]string, 0)
		}

		m[res.ManagerGroup] = append(arr, res.Name)
	}

	for managerName, usedServers := range used {
		manager := registry.ResourceManagers[managerName]
		if manager.FindUnused != nil {
			tmp := commandtree.NewRootCommand("")
			unused, err := manager.FindUnused(usedServers, tmp.AsCommand(), c)
			if err != nil {
				c.Err(err)
				return
			}
			if len(unused) > 0 {
				if len(unused) == 1 {
					c.Logf("Found unused %v server:", manager.Name)
				} else {
					c.Logf("Found %v unused %v servers:", len(unused), manager.Name)
				}
				for _, name := range unused {
					c.Logf(" - %v", name)
				}
				if c.allowDecommission {
					for _, child := range tmp.AsCommand().Children {
						c.Add(child.AsCommand().Caption, child)
					}
				} else if len(tmp.AsCommand().Children) > 0 {
					printInstruction = true
				}
			}
		}
	}

	if printInstruction {
		c.Logf("Use the flag --allowdecommission to automatically decommision unused servers")
	}
}
