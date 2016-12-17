package firewall

import (
	"fmt"
	"strings"
)

type iptables struct {
	run func(args ...string) (string, error)
}

type chain struct {
	DefaultPolicy string
	Rules         []rule
}

type rule []string

func (r rule) equal(remote rule) bool {
	if len(r) != len(remote) {
		return false
	}
	for i, v := range r {
		if v != remote[i] {
			return false
		}
	}
	return true
}

func (r rule) String() string {
	return strings.Join(r, " ")
}

func (r rule) spec() []string {
	return r
}

func (r rule) find(name string) string {
	ret := ""
	matching := false
	for _, v := range r {
		if v == name {
			matching = true
			continue
		}

		if matching {
			if strings.HasPrefix(v, "-") {
				break
			} else if ret == "" {
				ret = v
			} else {
				ret = ret + " " + v
			}
		}
	}
	return ret
}

func (i *iptables) sync(table string, prefix string, targetChains map[string]*chain, targetJumps map[string][]rule, defaultPolicy map[string]string) error {
	// get current chains
	currentChains, err := i.listChains(table)
	if err != nil {
		return err
	}

	// ensure all chains exist
	for chainName := range targetChains {
		if !strings.HasPrefix(chainName, prefix) {
			return fmt.Errorf("The chain '%v.%v' must start with prefix '%v'", table, chainName, prefix)
		}

		if _, found := currentChains[chainName]; !found {
			// chain not found... create it!
			output, err := i.run("-t", table, "-N", chainName)
			if err != nil {
				return fmt.Errorf("Could not create chain %v.%v Error: %v, Output:%v", table, chainName, err.Error(), output)
			}

			// chain created!
			currentChains[chainName] = &chain{Rules: make([]rule, 0, 0)}
		}
	}

	// update all chains
	for chainName, c := range targetChains {
		currentChain := currentChains[chainName]

		// ensure the chain consists of only the right rules
		for n, rule := range c.Rules {
			if n >= len(currentChain.Rules) {
				// append. easy.
				output, err := i.run(append([]string{"-t", table, "-A", chainName}, rule.spec()...)...)
				if err != nil {
					return fmt.Errorf("Could not create rule in %v.%v Error: %v, Output:%v", table, chainName, err.Error(), output)
				}
			} else {
				// compare if rule needs to be updated
				if !rule.equal(currentChain.Rules[n]) {
					output, err := i.run(append([]string{"-t", table, "-R", chainName, fmt.Sprintf("%v", n+1)}, rule.spec()...)...)
					if err != nil {
						return fmt.Errorf("Could not set chain %v.%v rule. Error: %v, Output:%v", table, chainName, err.Error(), output)
					}
				}
			}
		}

		// delete excess rules.
		excessRules := (len(currentChain.Rules) - len(c.Rules))
		for x := 0; x < excessRules; x++ {
			id := len(currentChain.Rules) - x
			output, err := i.run("-t", table, "-D", chainName, fmt.Sprintf("%v", id))
			if err != nil {
				return fmt.Errorf("Could not delete %v.%v rule #%v. Error: %v, Output:%v", table, chainName, id, err.Error(), output)
			}
		}
	}

	// check jump
	for chainName, jumps := range targetJumps {
		chain, found := currentChains[chainName]
		if !found {
			return fmt.Errorf("Could not locate the %v chain.", chainName)
		}

		// ensure ensure our jumps are the last part of the chain.
		valid := len(chain.Rules) > len(jumps)
		if valid {
			for n, jump := range jumps {
				if !chain.Rules[len(chain.Rules)-len(jump)+n].equal(jump) {
					valid = false
				}
			}
		}

		// if we have zero jumps, there shouldn't be any jumps with the given prefix.
		if len(jumps) == 0 {
			for _, r := range chain.Rules {
				if strings.HasPrefix(r.find("-j"), prefix) {
					valid = false
				}
			}
		}

		// remove all
		if !valid {
			// remove all jumps to prefix
			for n := len(chain.Rules) - 1; n >= 0; n-- {
				r := chain.Rules[n]
				if strings.HasPrefix(r.find("-j"), prefix) {
					output, err := i.run("-t", table, "-D", chainName, fmt.Sprintf("%v", n+1))
					if err != nil {
						return fmt.Errorf("Could not delete %v.%v rule #%v. Error: %v, Output:%v", table, chainName, n+1, err.Error(), output)
					}
				}
			}

			// add jumps
			for _, jump := range jumps {
				output, err := i.run(append([]string{"-t", table, "-A", chainName}, jump.spec()...)...)
				if err != nil {
					return fmt.Errorf("Could not create jump %v in filter.%v. Error: %v, Output:%v", jump, chainName, err.Error(), output)
				}
			}
		}
	}

	// delete any orphaned chains
	for name := range currentChains {
		if strings.HasPrefix(name, prefix) {
			if _, shouldExist := targetChains[name]; !shouldExist {
				// flush the chain
				output, err := i.run("-t", table, "-F", name)
				if err != nil {
					return fmt.Errorf("Could not flush chain %v.%v rule. Error: %v, Output:%v", table, name, err.Error(), output)
				}

				// delete the chain
				output, err = i.run("-t", table, "-X", name)
				if err != nil {
					return fmt.Errorf("Could not delete chain %v.%v rule. Error: %v, Output:%v", table, name, err.Error(), output)
				}
			}
		}
	}

	// set default policy
	for chainName, policy := range defaultPolicy {
		current, found := currentChains[chainName]
		if !found {
			return fmt.Errorf("The chain '%v.%v' could not be found. Need it to set policy to %v", table, chainName, defaultPolicy)
		}
		if current.DefaultPolicy != policy {
			output, err := i.run("-t", table, "-P", chainName, policy)
			if err != nil {
				return fmt.Errorf("Could not set default policy on chain %v.%v to %v. Error: %v, Output:%v", table, chainName, policy, err.Error(), output)
			}
		}
	}

	return nil
}

func (i *iptables) listChains(table string) (map[string]*chain, error) {
	// run command
	output, err := i.run("-t", table, "-S")
	if err != nil {
		return nil, err
	}

	// split output into array on newline
	arr := strings.Split(output, "\n")
	for i, v := range arr {
		arr[i] = strings.TrimSpace(v)
	}
	if len(arr) > 0 && arr[len(arr)-1] == "" {
		arr = arr[:len(arr)-1]
	}

	// build chains
	chains := make(map[string]*chain) // chain => rules
	for _, line := range arr {
		a := strings.Split(line, " ")
		switch a[0] {
		case "-P": // Policy for chain
			c, found := chains[a[1]]
			if !found {
				c = &chain{}
				chains[a[1]] = c
			}
			//c.DefaultPolicy = a[2]
			break
		case "-N": // New chain
			if _, found := chains[a[1]]; !found {
				chains[a[1]] = &chain{}
			}
			break
		case "-A": // Add rule
			c, found := chains[a[1]]
			if !found {
				c = &chain{}
				chains[a[1]] = c
			}
			c.Rules = append(c.Rules, rule(a[2:]))
			break
		}
	}
	return chains, nil
}
