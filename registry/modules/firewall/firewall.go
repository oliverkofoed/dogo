package firewall

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"os/user"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/oliverkofoed/dogo/commandtree"
	"github.com/oliverkofoed/dogo/registry/utilities"
	"github.com/oliverkofoed/dogo/schema"
	"github.com/oliverkofoed/dogo/snobgob"
)

type Firewall struct {
	Protocol  schema.Template `default:"tcp" description:"The protocol for this entry. Valid: 'tcp' or 'udp'."`
	Port      int             `required:"yes"`
	From      schema.Template `default:"" description:"comma seperated list of ips allowed access via this rule"`
	Interface schema.Template `default:"" description:"the interface name that allows access via this rule"`
	Skip      bool            `description:"Skip editing the firewall rule. Useful for having a local override setting skip=true to local development machine that does not have iptables."`
}

type state struct {
	Supported  bool
	ChainsIPV4 map[string]*chain
	ChainsIPV6 map[string]*chain
}

const prefix = "dogo_"

// Manager is the main entry point to this Dogo Module
var Manager = schema.ModuleManager{
	Name:            "firewall",
	ModulePrototype: &Firewall{},
	StatePrototype:  &state{},
	GobRegister: func() {
		snobgob.Register(rule{})
		snobgob.Register(chain{})
		snobgob.Register(syncFirewallCommand{})
	},
	GetState: func(query interface{}) (interface{}, error) {
		if runtime.GOOS == "darwin" {
			return &state{
				Supported: false,
			}, nil
		}

		chains4, err := getIPTablesCommand("iptables", nil).listChains("filter")
		if err != nil {
			return nil, err
		}

		chains6, err := getIPTablesCommand("ip6tables", nil).listChains("filter")
		if err != nil {
			return nil, err
		}

		return &state{
			Supported:  true,
			ChainsIPV4: chains4,
			ChainsIPV6: chains6,
		}, nil
	},
	CalculateCommands: func(c *schema.CalculateCommandsArgs) error {
		var err error
		remoteState := c.State.(*state)
		modules := c.Modules.([]*Firewall)
		cmd := &syncFirewallCommand{}

		if !remoteState.Supported {
			for _, module := range modules {
				if !module.Skip {
					return fmt.Errorf("Firewall not supported on the system.")
				}
			}
			return nil
		}

		// check if skip all
		if len(modules) > 0 {
			skipAll := true
			for _, module := range modules {
				skipAll = module.Skip && skipAll
			}
			if skipAll {
				return nil
			}
		}

		// calculate ipv4 rules
		cmd.IPV4TargetChains, cmd.IPV4TargetJumps, cmd.IPV4DefaultPolicy, cmd.IPV4Sync, err = buildCommand(c, false, modules, remoteState.ChainsIPV4)
		if err != nil {
			return err
		}

		// calculate ipv6 rules
		cmd.IPV6TargetChains, cmd.IPV6TargetJumps, cmd.IPV6DefaultPolicy, cmd.IPV6Sync, err = buildCommand(c, true, modules, remoteState.ChainsIPV6)
		if err != nil {
			return err
		}

		// check that there is at least one rule for port 22
		sshPortFound := false
		for _, m := range []map[string]*chain{cmd.IPV4TargetChains, cmd.IPV6TargetChains} {
			for _, c := range m {
				for _, r := range c.Rules {
					if r.find("--dport") == "22" {
						sshPortFound = true
						break
					}
				}
				if sshPortFound {
					break
				}
			}
			if sshPortFound {
				break
			}
		}
		if !sshPortFound {
			return errors.New("No rule found for port 22 (SSH). If you applied these rules, you wouldn't be able to SSH to the machine anymore.")
		}

		// sync rules if required.
		if cmd.IPV4Sync || cmd.IPV6Sync {
			c.RemoteCommands.Add("Adjust firewall rules (iptables)", cmd)
		}
		return nil
	},
}

func buildCommand(c *schema.CalculateCommandsArgs, isIPV6 bool, modules []*Firewall, remoteChains map[string]*chain) (targetChains map[string]*chain, targetJumps map[string][]rule, defaultPolicy map[string]string, doSync bool, bad error) {
	targetChains = make(map[string]*chain)
	targetJumps = make(map[string][]rule)
	defaultPolicy = make(map[string]string)
	used := make(map[string]bool)

	for _, module := range modules {
		if module.Skip {
			continue
		}

		fromString, err := module.From.Render(nil)
		if err != nil {
			return nil, nil, nil, false, err
		}
		iface, err := module.Interface.Render(nil)
		if err != nil {
			return nil, nil, nil, false, err
		}
		protocol, err := module.Protocol.Render(nil)
		if err != nil {
			return nil, nil, nil, false, err
		}
		if protocol == "" || !(protocol == "udp" || protocol == "tcp") {
			protocol = "tcp"
		}

		addresses := strings.Split(fromString, ",")
		for _, address := range addresses {
			trimmed := strings.TrimSpace(address)
			if trimmed != "" || len(addresses) == 1 {
				if trimmed != "" {
					ip := net.ParseIP(trimmed)
					if ip == nil {
						return nil, nil, nil, false, fmt.Errorf("Could not parse address '%v' from address list '%v' (values should be comma seperated)", trimmed, fromString)
					}

					if ip.To4() != nil {
						if isIPV6 {
							continue
						}
						if !strings.Contains(trimmed, "/") {
							trimmed = trimmed + "/32"
						}
					} else if ip.To4() == nil {
						if !isIPV6 {
							continue
						}
						if !strings.Contains(trimmed, "/") {
							trimmed = ip.String() + "/128"
						}
					}
				}

				// build rule
				r := rule{}

				if trimmed != "" {
					r = append(r, "-s", trimmed)
				}
				if iface != "" {
					r = append(r, "-i", iface)
				}
				r = append(r, "-j", "ACCEPT")

				// add rule to proper chain
				chainName := fmt.Sprintf("%v%v_%v", prefix, protocol, module.Port)
				key := chainName + ":" + r.String()
				if _, found := used[key]; !found {
					c, found := targetChains[chainName]
					if !found {
						c = &chain{Rules: make([]rule, 0)}
						targetChains[chainName] = c
					}
					c.Rules = append(c.Rules, r)
					used[key] = true
				}
			}
		}
	}

	for _, c := range targetChains {
		sort.Sort(stableRuleSort(c.Rules))
	}

	targetJumps["INPUT"] = []rule{}
	if len(targetChains) > 0 {
		inputRules := make([]rule, 0, len(targetChains))

		inputRules = append(inputRules, rule{"-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED", "-j", "ACCEPT"}) // allow traffic for established and related connections.
		inputRules = append(inputRules, rule{"-m", "conntrack", "--ctstate", "INVALID", "-j", "DROP"})               // drop packets that are invalid (FIN/SYN etc flags in bad combinations), since it's .. well, invalid and could single an attack.
		for _, chainName := range sortKeys(targetChains) {
			parts := strings.Split(chainName, "_")
			inputRules = append(inputRules, rule{"-p", parts[1], "-m", parts[1], "--dport", parts[2], "-j", chainName}) // jump to sub-chain for specific protocol+port
		}
		inputRules = append(inputRules, rule{"-i", "lo", "-j", "ACCEPT"}) // allow loopback traffic.

		targetChains["dogo_input"] = &chain{Rules: inputRules}
		targetJumps["INPUT"] = []rule{rule{"-j", "dogo_input"}}
		defaultPolicy["INPUT"] = "DROP"
		defaultPolicy["FORWARD"] = "DROP"
	}

	// start comparing calculated rules with remote rules
	doSync = false

	// any chains in remote that shouldn't be there?
	for chainName := range remoteChains {

		if strings.HasPrefix(chainName, prefix) {
			if _, found := targetChains[chainName]; !found {
				doSync = true
				break
			}
		}
	}

	// all chains in remote that should be there?
	if !doSync {
		for chainName, ch := range targetChains {
			remoteChain, found := remoteChains[chainName]
			if !found || len(ch.Rules) != len(remoteChain.Rules) {
				doSync = true
				break
			}
			for n, r := range ch.Rules {
				if !r.equal(remoteChain.Rules[n]) {
					doSync = true
					break
				}
			}
		}
	}

	// check jumps
	if !doSync {
		for chainName, jumpRules := range targetJumps {
			foundRules := 0
			for _, r := range remoteChains[chainName].Rules {
				for _, a := range jumpRules {
					if a.equal(r) {
						foundRules++
						break
					}
				}
			}
			if len(jumpRules) != foundRules {
				doSync = true
			}
		}
	}

	return
}

type stableRuleSort []rule

func (x stableRuleSort) Len() int      { return len(x) }
func (x stableRuleSort) Swap(i, j int) { x[i], x[j] = x[j], x[i] }
func (x stableRuleSort) Less(i, j int) bool {
	return strings.Compare(x[i].String(), x[j].String()) == -1
}

type syncFirewallCommand struct {
	commandtree.Command
	IPV4Sync          bool
	IPV4TargetChains  map[string]*chain
	IPV4TargetJumps   map[string][]rule
	IPV4DefaultPolicy map[string]string
	IPV6Sync          bool
	IPV6TargetChains  map[string]*chain
	IPV6TargetJumps   map[string][]rule
	IPV6DefaultPolicy map[string]string
}

func (c *syncFirewallCommand) Execute() {
	if c.IPV4Sync {
		fw := getIPTablesCommand("iptables", nil)
		err := fw.sync("filter", prefix, c.IPV4TargetChains, c.IPV4TargetJumps, c.IPV4DefaultPolicy)
		if err != nil {
			c.Errf("Error setting firewall rules: %v", err.Error())
		}
	}

	if c.IPV6Sync {
		fw := getIPTablesCommand("ip6tables", nil)
		err := fw.sync("filter", prefix, c.IPV6TargetChains, c.IPV6TargetJumps, c.IPV6DefaultPolicy)
		if err != nil {
			c.Errf("Error setting firewall rules: %v", err.Error())
		}
	}

	if c.IPV6Sync || c.IPV4Sync {
		// ensure iptables-persistent is installed
		cmd := exec.Command("service", "iptables-persistent")
		buf := bytes.NewBuffer(nil)
		cmd.Stdout = buf
		cmd.Stderr = buf
		if err := cmd.Run(); err != nil && !strings.Contains(buf.String(), "start|restart|reload|force-reload|save|flush") {
			c.Logf("Installing iptables-persistent")
			cmd := exec.Command("/bin/bash", "-c", "DEBIAN_FRONTEND=noninteractive apt-get -y install iptables-persistent")
			cmd.Stdout = commandtree.NewLogFuncWriter(" - ", c.Logf)
			cmd.Stderr = commandtree.NewLogFuncWriter(" - ", c.Logf)
			if err := utilities.MachineExclusive(cmd.Run); err != nil {
				c.Errf("Could not install iptables-persistent: %v", err)
				return
			}
		}

		// save the rules
		PersistRules(c)
	}
}

func PersistRules(l schema.Logger) {
	// ensure iptables-persistent is installed
	cmd := exec.Command("service", "iptables-persistent")
	buf := bytes.NewBuffer(nil)
	cmd.Stdout = buf
	cmd.Stderr = buf
	if err := cmd.Run(); err != nil && strings.Contains(buf.String(), "start|restart|reload|force-reload|save|flush") {
		// save ipv4 and ipv6 rules.
		for _, command := range []string{"iptables-save > /etc/iptables/rules.v4", "ip6tables-save > /etc/iptables/rules.v6"} {
			cmd := exec.Command("/bin/bash", "-c", command)
			cmd.Stdout = commandtree.NewLogFuncWriter("", l.Logf)
			cmd.Stderr = commandtree.NewLogFuncWriter("", l.Errf)
			if err := cmd.Run(); err != nil {
				l.Err(err)
				return
			}
		}
	}
}

func getIPTablesCommand(commandName string, owner *commandtree.Command) *iptables {
	myuser, _ := user.Current()
	uid, _ := strconv.Atoi(myuser.Uid)
	gid, _ := strconv.Atoi(myuser.Gid)

	return &iptables{run: func(args ...string) (string, error) {
		c := exec.Command(commandName, append([]string{"--wait"}, args...)...)
		var output bytes.Buffer
		c.Stdout = &output
		c.Stderr = &output
		c.SysProcAttr = &syscall.SysProcAttr{}
		c.SysProcAttr.Credential = &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}
		err := c.Run()
		if owner != nil {
			owner.Logf(" --- RUN: %v", strings.Join(append([]string{commandName, "--wait"}, args...), " "))
		}
		if err != nil {
			cmd := strings.Join(append([]string{commandName, "--wait"}, args...), " ")
			return output.String(), fmt.Errorf("Error: %v. Command: '%v'", err.Error(), cmd)
		}
		return output.String(), nil
	}}
}
func sortKeys(m map[string]*chain) []string {
	arr := make([]string, 0, len(m))
	for k := range m {
		arr = append(arr, k)
	}
	sort.Strings(arr)
	return arr
}
