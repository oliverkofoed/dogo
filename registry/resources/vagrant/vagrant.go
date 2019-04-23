package vagrant

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"regexp"
	"strconv"
	"sync"

	"fmt"
	"time"

	"io/ioutil"

	"path/filepath"

	"os"

	"net"
	"strings"

	"github.com/oliverkofoed/dogo/commandtree"
	"github.com/oliverkofoed/dogo/registry/resources/virtualbox"
	"github.com/oliverkofoed/dogo/schema"
	"github.com/oliverkofoed/dogo/snobgob"
	"github.com/oliverkofoed/dogo/ssh"
)

const cacheDir = ".dogocache/vagrant/"
const machineInfoFile = "Vagrantfile.dogo"

type Vagrant struct {
	Name          string
	Box           schema.Template   `required:"true" default:"ubuntu/trusty64" description:"The vagrant box to use"`
	PrivateIPs    []schema.Template `required:"false" description:"The private ips to assign to the machine (using NAT between guest and host)"`
	SharedFolders []schema.Template `required:"false" description:"Any shared folders between host and guest. Each entry should be in the form hostdir:guestdir"`
	Memory        int               `required:"false" description:"The amount of ram to dedicate to the virtual machine"`
	CPUs          int               `required:"false" description:"The number of CPUs in the virtual machine"`
	info          *machineInfo
}

type VagrantGroup struct {
	DecommissionTag string `required:"false" description:"Assign a tag to all servers. The tag will be used to decommission servers that have that tag, but aren't in the environment any longer."`
}

var boxLock = sync.RWMutex{}
var boxMap = make(map[string]*sync.Once)
var boxMapInitialized = false

// Manager is the main entry point for this resource type
var Manager = schema.ResourceManager{
	Name:              "vagrant",
	ResourcePrototype: &Vagrant{},
	GroupPrototype:    &VagrantGroup{},
	Provision: func(group interface{}, resource interface{}, l schema.Logger) error {
		g := group.(*VagrantGroup)
		s := resource.(*Vagrant)

		// fin the server label
		label := g.DecommissionTag
		if len(label) > 0 {
			label += "_"
		}
		label += s.Name

		// file paths
		dir := filepath.Join(cacheDir, label)
		vagrantfilePath := filepath.Join(dir, "Vagrantfile")
		machineInfoFilePath := filepath.Join(dir, machineInfoFile)

		// ensure folder exists
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}

		// calculate the vagrant file contents
		vagrantfile, err := vagrantfile(s, label, g.DecommissionTag)
		if err != nil {
			return err
		}

		// read machine info
		info := &machineInfo{}
		if b, err := ioutil.ReadFile(machineInfoFilePath); err == nil {
			decoder := snobgob.NewDecoder(bytes.NewReader(b))
			if err := decoder.Decode(info); err == nil {
				if bytes.Equal(info.Vagrantfile, vagrantfile) {
					if stdout, _, err := virtualbox.VMBoxManage("showvminfo", label); err == nil {
						started := strings.Contains(stdout, "State:                       running")
						if !started {
							l.Logf("Starting virtualbox VM directly (bypassing vagrant for speed)")
							if _, _, err := virtualbox.VMBoxManage("startvm", label, "--type", "headless"); err != nil {
								l.Logf(" - error starting virtualbox VM directly. falling back to vagrant.")
							} else {
								started = true
							}
						}

						if started {
							// wait for ssh.
							if err := ssh.WaitForSSH(info.SSHAddress, info.SSHPort, info.SSHUser, "", info.SSHKey, time.Millisecond*200); err == nil {
								s.info = info
								return nil
							}
						}
					}
				}
			}
		}

		// write vagrant file
		if err := ioutil.WriteFile(vagrantfilePath, vagrantfile, 0700); err != nil {
			return err
		}

		// ensure box exists.
		box, err := s.Box.Render(nil)
		if err != nil {
			return err
		}
		boxLock.Lock()
		if !boxMapInitialized {
			l.Logf("Listing vagrant boxes")
			cmd := exec.Command("vagrant", "box", "list")
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			if err != nil {
				return err
			}
			for _, line := range strings.Split(string(out), "\n") {
				parts := strings.Split(line, " ")
				o := &sync.Once{}
				o.Do(func() {})
				boxMap[parts[0]] = o
			}
			boxMapInitialized = true
		}
		o, found := boxMap[box]
		if !found {
			o = &sync.Once{}
			boxMap[box] = o
		}
		boxLock.Unlock()
		o.Do(func() {
			l.Logf("Getting box %v", box)
			cmd := exec.Command("vagrant", "box", "add", box)
			cmd.Dir = dir
			cmd.Stderr = commandtree.NewLogFuncWriter(" - ", l.Errf)
			cmd.Stdout = commandtree.NewLogFuncWriter(" - ", l.Logf)
			cmd.Run()
		})

		// run vagrant reload in the folder
		needToRunVagrantUp := true
		if len(info.Vagrantfile) == 0 {
			cmd := exec.Command("vagrant", "reload")
			cmd.Dir = dir
			cmd.Stderr = commandtree.NewLogFuncWriter("", l.Errf)
			cmd.Stdout = commandtree.NewLogFuncWriter("", func(output string, args ...interface{}) {
				needToRunVagrantUp = needToRunVagrantUp || strings.Contains(output, "VM not create")
				l.Logf(output, args)
			})
			if err = cmd.Run(); err != nil {
				return err
			}
		}

		// run vagrant up, if reload didn't work.
		if needToRunVagrantUp {
			cmd := exec.Command("vagrant", "up", "--provider", "virtualbox")
			cmd.Dir = dir
			cmd.Stdout = commandtree.NewLogFuncWriter("", l.Logf)
			cmd.Stderr = commandtree.NewLogFuncWriter("", l.Errf)
			if err = cmd.Run(); err != nil {
				return err
			}
		}

		// read SSH config from folder
		b := bytes.NewBuffer(nil)
		cmd := exec.Command("vagrant", "ssh-config")
		cmd.Dir = dir
		cmd.Stdout = b
		cmd.Stderr = b
		err = cmd.Run()
		if err != nil {
			return fmt.Errorf("Could not get ssh-config from vagrant: %v", err)
		}
		sshConfigRegexp := regexp.MustCompile("^\\s+([^\\s]+)\\s+([^\\s]+)")
		for _, line := range strings.Split(b.String(), "\n") {
			m := sshConfigRegexp.FindStringSubmatch(line)
			if len(m) == 3 {
				switch m[1] {
				case "HostName":
					info.SSHAddress = m[2]
					break
				case "IdentityFile":
					buf, err := ioutil.ReadFile(m[2])
					if err != nil {
						return fmt.Errorf("Could not read keyfile at %v. Error: %v", m[2], err)
					}
					info.SSHKey = buf
					break
				case "User":
					info.SSHUser = m[2]
					break
				case "Port":
					if p, err := strconv.Atoi(m[2]); err == nil {
						info.SSHPort = p
					}
					break
				}
			}
		}

		// ensure we have valid connection info
		if info.SSHAddress == "" || info.SSHKey == nil || info.SSHUser == "" || info.SSHPort == 0 {
			j, _ := json.Marshal(info)
			return fmt.Errorf("Invalid ssh configuration: %v", string(j))
		}

		// write info
		b.Reset()
		encoder := snobgob.NewEncoder(b)
		info.DecommissionTag = g.DecommissionTag
		info.Vagrantfile = vagrantfile
		if err := encoder.Encode(info); err == nil {
			ioutil.WriteFile(machineInfoFilePath, b.Bytes(), 0700)
		}

		s.info = info
		return nil
	},
	FindUnused: func(shouldExist map[interface{}][]string, decommisionRoot *commandtree.Command, l schema.Logger) ([]string, error) {
		unusedNames := make([]string, 0)

		// list all servers in vagrant
		files, err := filepath.Glob(cacheDir + "*")
		if err != nil {
			return nil, err
		}

		for _, dirname := range files {
			if b, err := ioutil.ReadFile(filepath.Join(dirname, machineInfoFile)); err == nil {
				decoder := snobgob.NewDecoder(bytes.NewReader(b))
				info := &machineInfo{}
				if err := decoder.Decode(info); err == nil {
					for groupIface, names := range shouldExist {
						if group, ok := groupIface.(*VagrantGroup); ok {
							if info.DecommissionTag == group.DecommissionTag {
								serverName := filepath.Base(dirname)

								found := false
								for _, name := range names {
									label := name
									if group.DecommissionTag != "" {
										label = group.DecommissionTag + "_" + name
									}

									if label == serverName {
										found = true
										break
									}
								}
								if !found {
									unusedNames = append(unusedNames, serverName)
								}
								break
							}
						}
					}
				}
			}
		}

		if len(unusedNames) > 0 {
			decommisionRoot.Add("Decommission vagrant servers", &decommisionCommand{labels: unusedNames})
		}
		return unusedNames, nil
	},
}

type decommisionCommand struct {
	commandtree.Command
	labels []string
}

func (c *decommisionCommand) Execute() {
	for _, label := range c.labels {
		c.Logf("decommissioning %v", label)
		serverDir := filepath.Join(cacheDir, label)
		b := bytes.NewBuffer(nil)
		cmd := exec.Command("vagrant", "destroy", "-f")
		cmd.Dir = serverDir
		cmd.Stdout = b
		cmd.Stderr = b
		err := cmd.Run()
		if err != nil {
			c.Errf("Could not get destroy vagrant server %v: %v (stdout/err: %v)", label, err, b.String())
			continue
		}
		err = os.RemoveAll(serverDir)
		if err != nil {
			c.Errf("Could not get delete vagrant dir %v: %v", serverDir, err)
			continue
		}
	}
}

type machineInfo struct {
	SSHAddress      string
	SSHPort         int
	SSHUser         string
	SSHKey          []byte
	Vagrantfile     []byte
	DecommissionTag string
}

func vagrantfile(s *Vagrant, label string, decommissionTag string) ([]byte, error) {
	b := bytes.NewBuffer(nil)
	fmt.Fprintf(b, "Vagrant.configure(2) do |config|\n")
	box, err := s.Box.Render(nil)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(b, "	# decommissiontag: %v\n", decommissionTag)
	fmt.Fprintf(b, "	config.vm.box = \"%v\"\n", box)
	fmt.Fprintf(b, "	config.vm.define \"%v\"\n", label)
	fmt.Fprintf(b, "	config.vm.provider \"virtualbox\" do |v|\n")
	if s.Memory > 0 {
		fmt.Fprintf(b, "		v.memory = \"%v\"\n", s.Memory)
	}
	if s.CPUs > 0 {
		fmt.Fprintf(b, "		v.cpus = %v\n", s.CPUs)
	}
	fmt.Fprintf(b, "		v.name = \"%v\"\n", label)
	fmt.Fprintf(b, "	end\n")
	for _, template := range s.PrivateIPs {
		ipString, err := template.Render(nil)
		if err != nil {
			return nil, err
		}
		ip := net.ParseIP(ipString)
		if ip == nil {
			return nil, fmt.Errorf("%v is not a valid ip", ipString)
		}
		fmt.Fprintf(b, "	config.vm.network \"private_network\", ip: \"%s\"\n", ipString)
	}
	for _, template := range s.SharedFolders {
		sharedDir, err := template.Render(nil)
		if err != nil {
			return nil, err
		}

		parts := strings.Split(sharedDir, ":")
		if len(parts) != 2 {
			return nil, fmt.Errorf("incorrect shared folder value '%v'. Should be in the form '<hostdir>:<guestdir>'", s)
		}
		hostPath := parts[0]
		guestPath := parts[1]
		hostPathAbs, err := filepath.Abs(hostPath)
		if err != nil {
			return nil, fmt.Errorf("Invalid local path for shared folder: %v", hostPath)
		}
		fmt.Fprintf(b, "	config.vm.synced_folder \"%v\", \"%v\"\n", hostPathAbs, guestPath)
	}
	fmt.Fprintf(b, "end\n")
	return b.Bytes(), nil
}

func (s *Vagrant) OpenConnection() (schema.ServerConnection, error) {
	return ssh.NewSSHConnection(s.info.SSHAddress, s.info.SSHPort, s.info.SSHUser, "", s.info.SSHKey, time.Second*30)
}
