package main

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"strings"

	"github.com/oliverkofoed/dogo/commandtree"
	"github.com/oliverkofoed/dogo/registry/resources/localhost"
	"github.com/oliverkofoed/dogo/schema"
	"github.com/oliverkofoed/dogo/term"
)

type tunnelState int

const (
	tunnelStateIdle tunnelState = iota
	tunnelStateConnecting
	tunnelStateConnected
	tunnelStateError
)

type dogoTunnelInfo struct {
	state              tunnelState
	name               string
	resource           *schema.Resource
	getConnection      func(l schema.Logger) (schema.ServerConnection, error)
	localport          int
	remoteport         int
	remotehost         string
	remotehostTemplate schema.Template
	tunnelstring       string
	err                error
}

func (d *dogoTunnelInfo) SetProgress(progress float64) {}
func (d *dogoTunnelInfo) Logf(format string, args ...interface{}) {
	if len(args) > 0 {
		format = fmt.Sprintf(format, args)
	}
	d.tunnelstring = format
}
func (d *dogoTunnelInfo) Errf(format string, args ...interface{}) {
	if len(args) > 0 {
		format = fmt.Sprintf(format, args)
	}
	d.Err(fmt.Errorf(format))
}
func (d *dogoTunnelInfo) Err(err error) {
	d.state = tunnelStateError
	d.err = err
}

type sortByTunnelAndServer []*dogoTunnelInfo

func (x sortByTunnelAndServer) Len() int      { return len(x) }
func (x sortByTunnelAndServer) Swap(i, j int) { x[i], x[j] = x[j], x[i] }
func (x sortByTunnelAndServer) Less(i, j int) bool {
	nameDiff := strings.Compare(x[i].name, x[j].name)
	if nameDiff == 0 {
		return strings.Compare(x[i].resource.Name, x[j].resource.Name) == -1
	}
	return nameDiff == -1
}

func dogoTunnel(config *schema.Config, environment *schema.Environment, query string) {
	setTemplateGlobals(config, environment)

	parts := strings.Split(query, ".")
	tunnels := make([]*dogoTunnelInfo, 0, 10)

	// grab the tunnel_offset, if specified in the config file
	portOffset := 0
	if offset, found := environment.Vars["tunnel_offset"]; found {
		if i, ok := offset.(int); ok {
			portOffset = i
		} else {
			fmt.Println(term.Red + fmt.Sprintf("tunnel_offset must be an integervalue. Got: %v (%T)", offset, offset) + term.Reset)
			return
		}
	}

	// build list of name, localport, remote port, server
	usedTunnel := make(map[string]bool)
	getStateMap := make(map[string][]*dogoTunnelInfo)
	for _, res := range environment.Resources {
		if server, ok := res.Resource.(schema.ServerResource); ok {
			getConnection := func(s schema.ServerResource, res *schema.Resource) func(l schema.Logger) (schema.ServerConnection, error) {
				var c schema.ServerConnection
				var err error
				var m sync.Mutex
				return func(l schema.Logger) (schema.ServerConnection, error) {
					m.Lock()
					defer m.Unlock()
					if err != nil {
						return c, err
					}
					if c == nil {
						// provision if required.
						if res.Manager.Provision != nil {
							err := res.Manager.Provision(res.ManagerGroup, res.Resource, &schema.PrefixLogger{Output: l, Prefix: "provision: "})
							if err != nil {
								return nil, err
							}
						}

						// open connection
						c, err = s.OpenConnection()
					}
					return c, err
				}
			}(server, res)

			for packageName := range res.Packages {
				pack := config.Packages[packageName]

				for tunnelName, tunnel := range pack.Tunnels {
					t := &dogoTunnelInfo{
						name:               tunnelName,
						resource:           res,
						localport:          tunnel.Port + portOffset,
						remoteport:         tunnel.Port,
						remotehostTemplate: tunnel.Host,
						getConnection:      getConnection,
					}

					// try to get the tunnel host, if it fails, mark that we'll
					// try to get the server state before opening any tunnels.
					host, err := t.remotehostTemplate.Render(nil)
					if err != nil {
						getStateMap[res.Name] = append(getStateMap[res.Name], t)
					}
					t.remotehost = host

					if len(parts) == 2 { // server.tunnel
						if res.Name == parts[1] && tunnelName == parts[2] {
							tunnels = append(tunnels, t)
						}
					} else if parts[0] == "*" { // all tunnels
						tunnels = append(tunnels, t)
					} else if parts[0] == "" { // one of each tunnel
						if _, found := usedTunnel[tunnelName]; !found {
							tunnels = append(tunnels, t)
							usedTunnel[tunnelName] = true
						}
					} else if parts[0] == res.Name { // all tunnels on server
						tunnels = append(tunnels, t)
					} else if parts[0] == tunnelName { // start the tunnel for this server
						tunnels = append(tunnels, t)
					}
				}
			}
		}
	}

	if len(tunnels) == 0 {
		fmt.Println(term.Red + "no tunnels matched." + term.Reset)
		return
	}

	// sort list of tunnel
	sort.Sort(sortByTunnelAndServer(tunnels))

	// get any state required by those tunnels.
	if len(getStateMap) > 0 {
		root := commandtree.NewRootCommand("Some tunnels require that we get state from remote servers")

		for _, v := range getStateMap {
			root.Add("Get state: "+environment.Name+"."+v[0].resource.Name, &dogoTunnelGetStateCommand{tunnels: v})
		}

		r := commandtree.NewRunner(root, 1)
		go r.Run(nil)
		if err := commandtree.ConsoleUI(root); err != nil {
			return
		}
	}

	// start a list of workers.
	usedPort := make(map[int]bool)
	usedPortLock := sync.Mutex{}
	workChan := make(chan *dogoTunnelInfo, len(tunnels))
	for i := 0; i != 1; i++ {
		go func() {
			for tunnel := range workChan {
				tunnel.state = tunnelStateConnecting

				// grab a connection
				connection, err := tunnel.getConnection(tunnel)
				if err != nil {
					tunnel.err = err
					tunnel.state = tunnelStateError
					break
				}

				/*tunnelHost, err := tunnel.remotehost.Render(nil)
				if err != nil {
					// possibly because we don't have the state

				}*/

				for x := 0; true; x++ {
					// after 80 tries, we're not going to find an avalible port
					if x >= 80 {
						tunnel.err = fmt.Errorf("Unable to listen. Last tried: %v. Last error: %v", tunnel.localport, tunnel.err)
						tunnel.state = tunnelStateError
						break
					}

					// find a port we haven't tried yet. (don't need to do this for localhost)
					if _, local := tunnel.resource.Resource.(*localhost.Localhost); !local {
						usedPortLock.Lock()
						for {
							if _, used := usedPort[tunnel.localport]; !used {
								usedPort[tunnel.localport] = true
								break
							} else {
								tunnel.localport++
							}
						}
						usedPortLock.Unlock()
					}

					// try starting a tunnel
					port, err := connection.StartTunnel(tunnel.localport, tunnel.remoteport, tunnel.remotehost, false)
					if err != nil {
						tunnel.localport++
						tunnel.err = err
						tunnel.state = tunnelStateError
						continue
					}

					// everything is great!
					tunnel.localport = port
					tunnel.tunnelstring = fmt.Sprintf("127.0.0.1:%v", tunnel.localport)
					tunnel.state = tunnelStateConnected
					break
				}
			}
		}()
	}

	// start the status printer
	go printTunnelStatus(environment, tunnels)

	// feed all the work into the workChan
	for _, tunnel := range tunnels {
		workChan <- tunnel
	}

	// wait for completion
	reader := bufio.NewReader(os.Stdin)
	_, _ = reader.ReadString('\n')
}

type dogoTunnelGetStateCommand struct {
	commandtree.Command
	tunnels []*dogoTunnelInfo
}

func (c *dogoTunnelGetStateCommand) Execute() {
	t := c.tunnels[0]
	connection, err := t.getConnection(c)
	if err != nil {
		c.Err(err)
		return
	}

	_, _, success := getState(t.resource, connection, false, c, c)
	if !success {
		return
	}

	// expand templates
	expandErrors := expandResourceTemplates(t.resource, config)
	if len(expandErrors) > 0 {
		for _, err := range expandErrors {
			c.Err(err)
		}
		return
	}

	for _, t := range c.tunnels {
		host, err := t.remotehostTemplate.Render(map[string]interface{}{
			"self": t.resource.Data,
		})
		if err != nil {
			c.Err(err)
			return
		}
		t.remotehost = host
	}
}

func printTunnelStatus(environment *schema.Environment, tunnels []*dogoTunnelInfo) {
	spinIndex := 0
	rewrite := false
	for {
		term.StartBuffer()

		if rewrite {
			term.MoveUp(4 + len(tunnels) + 2)
		}

		// pick spinner
		spinIndex++
		spinChar := string(commandtree.SpinnerCharacters[spinIndex%len(commandtree.SpinnerCharacters)])

		// calculate width
		maxLenTunnel := 0
		maxLenServer := 0
		maxLenStatus := 0
		for _, t := range tunnels {
			if len(t.name) > maxLenTunnel {
				maxLenTunnel = len(t.name)
			}
			if len(t.resource.Name) > maxLenServer {
				maxLenServer = len(t.resource.Name)
			}
			if len(t.tunnelstring) > maxLenStatus {
				maxLenStatus = len(t.tunnelstring)
			}
		}
		width := maxLenTunnel + maxLenServer + maxLenStatus
		if width < 50 {
			width = 50
		}

		// print header
		term.Print(term.White + runChar(dashes, width) + term.Reset + "\n")
		leftdashes := runChar(dashes, (width-27-len(environment.Name))/2)
		rightdashes := leftdashes
		if len(environment.Name)%2 == 0 {
			rightdashes += "-"
		}
		term.Print(term.White + leftdashes + "[ tunnels to " + term.Yellow + environment.Name + term.White + " environment ]" + rightdashes + term.Reset + "\n")
		term.Print(term.White + runChar(dashes, width) + term.Reset + "\n")

		col1 := maxLenTunnel + 5
		col2 := maxLenServer + 5
		col3 := maxLenStatus + 5
		term.Print(term.White)
		term.Print("tunnel")
		term.Print(runChar(spaces, 2+col1-len("tunnel")))
		term.Print("server")
		term.Print(runChar(spaces, col2-len("server")))
		term.Print(runChar(spaces, col3-len("status")))
		term.Print("status")
		term.Print(term.Reset + "\n")
		allDone := true
		for _, t := range tunnels {
			// waiting, connecting. connected, or failed.
			switch t.state {
			case tunnelStateIdle:
				term.Print(term.Reset)
				term.Print("  ")
				allDone = false
				break
			case tunnelStateConnecting:
				term.Print(term.Yellow)
				term.Print(spinChar)
				term.Print(" ")
				allDone = false
				break
			case tunnelStateConnected:
				term.Print(term.Green)
				term.Print("✓ ")
				term.Print(term.White)
				break
			case tunnelStateError:
				term.Print(term.Red)
				term.Print("! ")
				break
			}

			term.Print(t.name)
			term.Print(runChar(spaces, col1-len(t.name)))
			term.Print(t.resource.Name)
			term.Print(runChar(spaces, col2-len(t.resource.Name)))

			last := t.tunnelstring
			if t.state == tunnelStateError && t.err != nil {
				last = t.err.Error()
			}

			term.Print(runChar(spaces, col3-len(last)))
			term.Print(last)

			term.Print(term.Reset)
			term.Print("\n")
		}
		term.Print(term.White + runChar(dashes, width) + term.Reset + "\n")
		term.Print("(press enter to exit.)\n")
		term.FlushBuffer(true)
		time.Sleep(time.Millisecond * 200)
		rewrite = true
		if allDone {
			return
		}
	}
}
