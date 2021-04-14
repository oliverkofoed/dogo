package virtualbox

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"

	"time"

	"github.com/oliverkofoed/dogo/commandtree"
	"github.com/oliverkofoed/dogo/schema"
)

var guestPropertyRegexp = regexp.MustCompile("Name: (.*), value: (.*), timestamp: (.*), flags:")
var showVMInfoRegexp = regexp.MustCompile("(?P<name>[^:]+):\\s+(?P<value>.*)")

type Virtualbox struct {
	Name string
	//Image           schema.Template   `required:"true" default:"ubuntu/trusty64" description:"The vagrant box to use"`
	Image      string            `required:"true" default:"https://cloud-images.ubuntu.com/releases/16.04/release/ubuntu-16.04-server-cloudimg-amd64.ova" description:"path or url of the virtual box image (.ova) to use."`
	PrivateIPs []schema.Template `required:"false" description:"The private ips to assign to the machine (using NAT between guest and host)"`
	//SharedFolders []schema.Template `required:"false" description:"Any shared folders between host and guest. Each entry should be in the form hostdir:guestdir"`
	Networks int `required:"false" description:"The number of netwok interfaces"`
	Memory   int `required:"false" description:"The amount of ram to dedicate to the virtual machine"`
	CPUs     int `required:"false" description:"The number of CPUs in the virtual machine"`
	Storage  int `required:"false" description:"The size in mb of the storage in the virtual machine"`
	//info    *machineInfo
}

type VirtualboxGroup struct {
	DecommissionTag string `required:"false" description:"Assign a tag to all servers. The tag will be used to decommission servers that have that tag, but aren't in the environment any longer."`
}

var boxLock = sync.RWMutex{}
var boxMap = make(map[string]*sync.Once)
var boxMapInitialized = false

// Manager is the main entry point for this resource type
var Manager = schema.ResourceManager{
	Name:              "virtualbox",
	ResourcePrototype: &Virtualbox{},
	GroupPrototype:    &VirtualboxGroup{},
	Provision: func(group interface{}, resource interface{}, l schema.Logger) error {
		g := group.(*VirtualboxGroup)
		s := resource.(*Virtualbox)

		// find the server label
		label := g.DecommissionTag
		if len(label) > 0 {
			label += "_"
		}
		label += s.Name

		// collect machine state
		exists := true
		vminfoArr, err := vmBoxManageMatch(showVMInfoRegexp, "showvminfo", label)
		vminfo := make(map[string]string)
		if err != nil {
			if strings.Contains(err.stderr, "Could not find a registered machine named") {
				exists = false
			} else {
				return err
			}
		} else {
			for _, m := range vminfoArr {
				vminfo[m["name"]] = m["value"]
			}
		}

		// create machine if it does not exist.
		if !exists {
			ovaPath := s.Image
			if _, err := url.Parse(s.Image); err == nil {
				// calculate image name in cache.
				h := sha256.New()
				io.WriteString(h, s.Image)
				filename := filepath.Base(s.Image)
				ovaPath = fmt.Sprintf(".dogocache/virtualbox/ova/%x-%v", h.Sum(nil), filename)

				// download image if not in cache
				if _, err = os.Stat(ovaPath); os.IsNotExist(err) {
					l.Logf("Downloading image: %v", s.Image)

					//TODO: Ensure that only one download of this file happens at a time. (lock!)

					// start the download
					response, err := http.Get(s.Image)
					if err != nil {
						return err
					}
					defer response.Body.Close()

					// create file to receive download
					downloadpath := ovaPath + ".dl"
					err = os.MkdirAll(filepath.Dir(downloadpath), 0777)
					if err != nil {
						return err
					}
					f, err := os.Create(downloadpath)
					if err != nil {
						return err
					}
					defer f.Close()

					// process download
					reader := &progressReader{reader: response.Body, length: response.ContentLength}
					go func() {
						for reader.progress < 1 {
							l.SetProgress(reader.progress)
							time.Sleep(time.Second)
						}
						l.SetProgress(0)
					}()
					_, err = io.Copy(f, reader)
					if err != nil {
						return err
					}

					// download complete, rename!
					fmt.Println("moving from ", downloadpath, "to", ovaPath)
					err = os.Rename(downloadpath, ovaPath)
					if err != nil {
						return err
					}
				}
			} else {
				if _, err := os.Stat(ovaPath); os.IsNotExist(err) {
					return fmt.Errorf("could not find .ova image at %v", ovaPath)
				}
			}

			// import the imagea
			l.Logf("importing virtual machine image")
			if _, _, err := VMBoxManage("import", ovaPath,
				"--vsys", "0",
				"--vmname", label,
				//"--memory", fmt.Sprintf("%v", s.Memory),
				//"--unit", "5", "--ignore", // sound card
				//"--unit", "6", "--ignore", // usb
				//"--unit", "7", "--ignore", // floppy
			); err != nil {
				return err
			}
		}
		// check the machine network cards
		for i := 1; i != 9; i++ {
			current := vminfo[fmt.Sprintf("NIC %v", i)]
			if i == 1 || i <= s.Networks {
				if !strings.Contains(current, "Attachment: NAT") {
					l.Logf(" - configuring NIC #%v", i)
					if _, _, err := changeConfig(l, vminfo, "modifyvm", label, fmt.Sprintf("--nic%v", i), "nat"); err != nil {
						return err
					}
				}
			} else if current != "disabled" && current != "" {
				l.Logf(" - removing NIC #%v", i)
				if _, _, err := changeConfig(l, vminfo, "modifyvm", label, fmt.Sprintf("--nic%v", i), "none"); err != nil {
					return err
				}
			}
		}

		for k, v := range vminfo {
			fmt.Println(k+":", v)
		}

		// check memory
		if vminfo["Memory size"] != fmt.Sprintf("%vMB", s.Memory) {
			l.Logf(" - setting memory to %v", s.Memory)
			if _, _, err := changeConfig(l, vminfo, "modifyvm", label, "--memory", fmt.Sprintf("%v", s.Memory)); err != nil {
				return err
			}
		}

		// check cpus
		if s.CPUs < 1 {
			s.CPUs = 1
		}
		if vminfo["Number of CPUs"] != fmt.Sprintf("%v", s.CPUs) {
			l.Logf(" - setting cpus to %v", s.CPUs)
			if _, _, err := changeConfig(l, vminfo, "modifyvm", label, "--cpus", fmt.Sprintf("%v", s.CPUs)); err != nil {
				return err
			}
		}

		// start the vm
		if !strings.HasPrefix(vminfo["State"], "running") {
			l.Logf("starting virtual machine")
			if _, _, err := VMBoxManage("startvm", label, "--type", "headless"); err != nil {
				return err
			}
		}
		return nil
	},
	FindUnused: func(shouldExist map[interface{}][]string, decommisionRoot *commandtree.Command, l schema.Logger) ([]string, error) {
		return []string{}, errors.New("Not implemented yet")
	},
}

func (s *Virtualbox) OpenConnection() (schema.ServerConnection, error) {
	return nil, errors.New("Not implemented yet")
	//return ssh.NewSSHConnection(s.info.SSHAddress, s.info.SSHPort, s.info.SSHUser, "", s.info.SSHKey, time.Second*30)
}

type progressReader struct {
	reader   io.Reader
	received int64 // Total # of bytes transferred
	length   int64 // Expected length
	progress float64
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.reader.Read(b)
	if n > 0 {
		p.received += int64(n)
		p.progress = float64(p.received) / float64(p.length)
	}
	return n, err
}

func vmBoxManageMatch(regexp *regexp.Regexp, args ...string) ([]map[string]string, *VMBoxManageError) {
	stdout, _, err := VMBoxManage(args...)
	if err != nil {
		return nil, err
	}

	names := regexp.SubexpNames()
	lines := strings.Split(stdout, "\n")
	result := make([]map[string]string, 0, len(lines))
	for _, line := range lines {
		matches := regexp.FindStringSubmatch(line)
		if len(matches) >= len(names) {
			m := make(map[string]string)
			for x, name := range names {
				m[name] = matches[x]
			}
			result = append(result, m)
		}
	}
	return result, nil
}

func changeConfig(l schema.Logger, vminfo map[string]string, args ...string) (string, string, *VMBoxManageError) {
	if len(vminfo) != 0 && !strings.HasPrefix(vminfo["State"], "powered off") {
		l.Logf(" - stopping vm to apply configuration changes")
		if _, _, err := VMBoxManage("controlvm", vminfo["Name"], "poweroff"); err != nil {
			return "", "", err
		}
		vminfo["State"] = "powered off"
	}

	return VMBoxManage(args...)
}
func VMBoxManage(args ...string) (string, string, *VMBoxManageError) {
	// find vmbox manage tool.
	exe := ""
	switch runtime.GOOS {
	case "darwin":
		exe = "/usr/local/bin/VBoxManage"
		break
	case "linux":
		exe = "/usr/bin/VBoxManage"
		break
	case "windows":
		exe = filepath.Join(os.Getenv("PROGRAMFILES"), "Oracle", "VirtualBox", "VBoxManage.exe")
		break
	}
	if path, err := exec.LookPath("VBoxManage"); err == nil {
		exe = path
	}

	if _, err := os.Stat(exe); os.IsNotExist(err) || exe == "" {
		return "", "", &VMBoxManageError{stderr: "", stdout: "", err: "Could not locate vmboxmanage on this machine. Is Virtualbox installed", command: "vmboxmanage " + strings.Join(args, " ")}
	}

	cmd := exec.Command(exe, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return "", "", &VMBoxManageError{stderr: stderr.String(), stdout: stdout.String(), err: err.Error(), command: "vmboxmanage " + strings.Join(args, " ")}
	}
	return stdout.String(), stderr.String(), nil
}

type VMBoxManageError struct {
	stderr  string
	stdout  string
	err     string
	command string
}

func (e *VMBoxManageError) Error() string {
	return fmt.Sprintf("error(%v): %v. Stderr: %v", e.command, e.err, e.stderr)
}
