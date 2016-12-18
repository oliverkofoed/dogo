package linode

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"regexp"

	"github.com/oliverkofoed/dogo/commandtree"
	"github.com/oliverkofoed/dogo/schema"
	"github.com/oliverkofoed/dogo/snobgob"
	"github.com/oliverkofoed/dogo/ssh"
)

const cacheDir = ".dogocache/linode/"

type Linode struct {
	Name          string
	Datacenter    schema.Template `required:"true" description:"The datacenter to host the linode in."`
	Plan          schema.Template `required:"true" description:"The linode plan to use (server size)"`
	Distribution  schema.Template `required:"true" description:"The distribution to use for the OS partition"`
	Disks         schema.Template `required:"true" default:"swap:256" description:"How the disks should be configured"`
	Kernel        schema.Template `required:"true" defaultdescription:"Which kernel is the system using"`
	SSHPrivateKey schema.Template `required:"true" defaultdescription:"The private key used to authenticate against to the server"`
	SSHPublicKey  schema.Template `required:"true" defaultdescription:"The public key used to authenticate "`
	RootPassword  schema.Template `default:"" defaultdescription:"The password to set for the root user. If none given, will create a random 32 char password."`
	PrivateIPs    int             `required:"true" default:"0" defaultdescription:"How many private ips to assign to the machine"`
	info          *machineInfo
}

type machineInfo struct {
	PublicIPs  []string
	PrivateIPs []string
}

func (s *Linode) OpenConnection() (schema.ServerConnection, error) {
	privateKey, err := s.SSHPrivateKey.RenderFileBytes(nil)
	if err != nil {
		return nil, err
	}

	if s.info == nil {
		return nil, errors.New("Linode servers must be provisioned before every use")
	}

	return ssh.NewSSHConnection(s.info.PublicIPs[0], 22, "root", "", privateKey, time.Second*30)
}

type LinodeGroup struct {
	APIKey          schema.Template `required:"true" description:"The api key for the linode account"`
	DecommissionTag string          `required:"false" description:"Assign a tag to all servers. The tag will be used to decommission servers that have that tag, but aren't in the environment any longer."`
}

var decommissionTagRegex = regexp.MustCompile("^[a-z]{2,20}$")

func (g *LinodeGroup) Validate() error {
	if !decommissionTagRegex.MatchString(g.DecommissionTag) {
		return fmt.Errorf("DecommissionTag must only consist of 2-20 lowercase letters (a-z). got: '%v'", g.DecommissionTag)
	}
	return nil
}

var accountsLock sync.RWMutex
var accounts = make(map[string]*linodeAccount)
var oneList sync.Once
var nodeList []linode

// Manager is the main entry point for this resource type
var Manager = schema.ResourceManager{
	Name:              "linode",
	ResourcePrototype: &Linode{},
	GroupPrototype:    &LinodeGroup{},
	Provision: func(group interface{}, resource interface{}, l schema.Logger) error {
		g := group.(*LinodeGroup)
		s := resource.(*Linode)

		// get the api key.
		apikey, err := g.APIKey.Render(nil)
		if err != nil {
			return err
		}

		// fin the server label
		label := g.DecommissionTag
		if len(label) > 0 {
			label += "_"
		}
		label += s.Name

		// ensure folder exists
		if err := os.MkdirAll(cacheDir, 0700); err != nil {
			return err
		}

		// get private key and root password
		privateKey, err := s.SSHPrivateKey.RenderFileBytes(nil)
		if err != nil {
			return err
		}

		// load .dogocache/linode/(label.name)
		info := &machineInfo{}
		machineInfoFilePath := filepath.Join(cacheDir, label)
		if b, err := ioutil.ReadFile(machineInfoFilePath); err == nil {
			decoder := snobgob.NewDecoder(bytes.NewReader(b))
			if err := decoder.Decode(info); err == nil {
				if err := ssh.WaitForSSH(info.PublicIPs[0], 22, "root", "", privateKey, time.Millisecond*200); err == nil {
					s.info = info
					return nil
				}
			}
		}

		// get the manager for that account
		accountsLock.Lock()
		account, found := accounts[apikey]
		if !found {
			account = &linodeAccount{client: &client{apikey: apikey}}
			accounts[apikey] = account
		}
		accountsLock.Unlock()

		// list servers.
		servers, err := account.listServers(l)

		// create the server if it was not found.
		server, found := servers[label]
		if !found {
			l.Logf("Creating new server")

			// find the public ssh key
			publicKey, err := s.SSHPublicKey.RenderFileBytes(nil)
			if err != nil {
				return err
			}

			// find the root password
			rootPassword, err := s.RootPassword.Render(nil)
			if err != nil {
				return err
			}
			if rootPassword == "" {
				rootPassword, err = ssh.GenerateRandomPassword(32)
				if err != nil {
					return err
				}
			}

			err = account.create(s, label, l, rootPassword, string(publicKey))
			if err != nil {
				l.Errf("Could not create server %v: %v", s.Name, err)

				// remove the node that was created, if any
				if nodes, err := account.listServers(l); err == nil {
					for _, node := range nodes {
						if node.LABEL == label {
							l.Logf("Trying to remove half-backed server %v", node.LABEL)
							account.decommissionServer(node, l)
						}
					}
				}

				return errors.New("Could not create server.")
			}

			// list servers.
			servers, err = account.listServers(l)

			// create the server if it was not found.
			server, found = servers[label]
			if !found {
				return errors.New("could not find server in linode (linode api listLinode operation) right after creation. This should never happen")
			}
		}

		// create stored info
		info = &machineInfo{
			PublicIPs:  make([]string, 0),
			PrivateIPs: make([]string, 0),
		}

		// get the ip
		ips, err := account.client.lindeIPList(server.LINODEID)
		if err != nil {
			return fmt.Errorf("Could not get ip for server %v: %v", s.Name, err)
		}
		for _, ip := range ips {
			if ip.ISPUBLIC == 1 {
				info.PublicIPs = append(info.PublicIPs, ip.IPADDRESS)
			} else {
				info.PrivateIPs = append(info.PrivateIPs, ip.IPADDRESS)
			}
		}

		// write info to disk
		b := bytes.NewBuffer(nil)
		encoder := snobgob.NewEncoder(b)
		if err := encoder.Encode(info); err == nil {
			ioutil.WriteFile(machineInfoFilePath, b.Bytes(), 0700)
		}
		s.info = info

		// wait for machine to be ssh accessible.
		ssh.WaitForSSH(info.PublicIPs[0], 22, "root", "", privateKey, time.Second*30)

		return nil
	},
	FindUnused: func(shouldExist map[interface{}][]string, decommisionRoot *commandtree.Command, l schema.Logger) ([]string, error) {
		unusedNames := make([]string, 0)

		for groupIface, names := range shouldExist {
			if group, ok := groupIface.(*LinodeGroup); ok {
				// get the api key.
				apikey, err := group.APIKey.Render(nil)
				if err != nil {
					return nil, err
				}

				// get the manager for that account
				accountsLock.Lock()
				account, found := accounts[apikey]
				if !found {
					account = &linodeAccount{client: &client{apikey: apikey}}
					accounts[apikey] = account
				}
				accountsLock.Unlock()

				// list servers in that account
				servers, err := account.listServers(l)
				if err != nil {
					return nil, err
				}

				for _, node := range servers {
					if strings.HasPrefix(node.LABEL, group.DecommissionTag+"_") {
						found := false
						for _, name := range names {
							if node.LABEL == group.DecommissionTag+"_"+name {
								found = true
								break
							}
						}

						if !found {
							unusedNames = append(unusedNames, node.LABEL)
							decommisionRoot.Add("Decommission "+node.LABEL, &decommisionCommand{account: account, node: node})
						}
					}
				}
			}
		}

		return unusedNames, nil
	},
}

type decommisionCommand struct {
	commandtree.Command
	account *linodeAccount
	node    linode
}

func (c *decommisionCommand) Execute() {
	err := c.account.decommissionServer(c.node, c)
	if err != nil {
		c.Err(err)
	}
}

type linodeAccount struct {
	sync.RWMutex
	client          *client
	listServerCache map[string]linode
}

func (a *linodeAccount) decommissionServer(node linode, l schema.Logger) error {
	// shutdown
	l.Logf("shutting down")
	err := a.client.linodeShutdown(node.LINODEID)
	if err != nil {
		return err
	}
	err = a.waitForJobsToFinish(node.LINODEID, time.Minute*4, "", l)
	if err != nil {
		return err
	}

	// list disks
	l.Logf("listing disks")
	disks, err := a.client.linodeDiskList(node.LINODEID)
	if err != nil {
		return err
	}

	// delete disks
	for _, d := range disks {
		l.Logf("deleting disk: %v", d.DISKID)
		err := a.client.linodeDiskDelete(node.LINODEID, d.DISKID)
		if err != nil {
			return err
		}
	}
	err = a.waitForJobsToFinish(node.LINODEID, time.Minute*10, "", l)
	if err != nil {
		return err
	}

	// delete node.
	l.Logf("deleting linode")
	err = a.client.linodeDelete(node.LINODEID)
	a.listServerCache = nil
	if err != nil {
		return err
	}

	return nil
}

func (a *linodeAccount) listServers(l schema.Logger) (map[string]linode, error) {
	a.Lock()
	defer a.Unlock()

	if a.listServerCache != nil {
		return a.listServerCache, nil
	}

	list, err := a.client.linodeList()
	if err != nil {
		return nil, err
	}

	m := make(map[string]linode)
	for _, node := range list {
		m[node.LABEL] = node
	}
	a.listServerCache = m
	return a.listServerCache, nil
}

var infoOnce sync.Once
var initError error
var datacenters []datacenter
var distributions []distribution
var kernels []kernel
var plans []plan

func (a *linodeAccount) create(s *Linode, label string, l schema.Logger, rootPassword string, rootSSHKey string) error {
	// load initial data in parallel.
	infoOnce.Do(func() {
		var wg sync.WaitGroup
		wg.Add(4)
		go func() {
			if d, err := a.client.availDatacenters(); err == nil {
				datacenters = d
			} else {
				initError = err
			}
			wg.Done()
		}()
		go func() {
			if p, err := a.client.availLinodeplans(); err == nil {
				plans = p
			} else {
				initError = err
			}
			wg.Done()
		}()
		go func() {
			if d, err := a.client.availDistributions(); err == nil {
				distributions = d
			} else {
				initError = err
			}
			wg.Done()
		}()
		go func() {
			if k, err := a.client.availKernels(); err == nil {
				kernels = k
			} else {
				initError = err
			}
			wg.Done()
		}()
		wg.Wait()
	})
	if initError != nil {
		return fmt.Errorf("Could not load initial data from linode: %v", initError)
	}

	// find datacenter
	datacenterID := -1
	wantedDataCenter, err := s.Datacenter.Render(nil)
	if err != nil {
		return err
	}
	for _, dc := range datacenters {
		if dc.ABBR == wantedDataCenter {
			datacenterID = dc.DATACENTERID
		}
	}
	if datacenterID == -1 {
		avaliableDataCenters := make([]string, 0, len(datacenters))
		for _, dc := range datacenters {
			avaliableDataCenters = append(avaliableDataCenters, dc.ABBR)
		}
		return fmt.Errorf("'%v' is not a valid datacenter. Avaliable data centers: [%v]", wantedDataCenter, strings.Join(avaliableDataCenters, ","))
	}

	// find plan
	var plan plan
	wantedPlan, err := s.Plan.Render(nil)
	if err != nil {
		return err
	}
	for _, p := range plans {
		if planString(&p) == wantedPlan {
			plan = p
		}
	}
	if plan.PLANID == 0 {
		avaliablePlans := make([]string, 0, len(plans))
		for _, p := range plans {
			avaliablePlans = append(avaliablePlans, planString(&p))
		}
		return fmt.Errorf("%'v' is not a valid plan. Avaliable plans: [%v]", wantedPlan, strings.Join(avaliablePlans, ","))
	}

	// find kernel
	kernelID := -1
	wantedKernel, err := s.Kernel.Render(nil)
	if err != nil {
		return err
	}
	for _, k := range kernels {
		if k.LABEL == wantedKernel {
			kernelID = k.KERNELID
		}
	}
	if kernelID == -1 {
		avaliableKernels := make([]string, 0, len(kernels))
		for _, k := range kernels {
			avaliableKernels = append(avaliableKernels, k.LABEL)
		}
		return fmt.Errorf("'%v' is not a valid kernel. Avaliable kernels: [%v]", wantedKernel, strings.Join(avaliableKernels, ","))
	}

	// find distribution
	var dist distribution
	wantedDistribution, err := s.Distribution.Render(nil)
	if err != nil {
		return err
	}
	for _, d := range distributions {
		if d.LABEL == wantedDistribution {
			dist = d
		}
	}
	if dist.DISTRIBUTIONID == 0 {
		avaliableDistributions := make([]string, 0, len(distributions))
		for _, d := range distributions {
			avaliableDistributions = append(avaliableDistributions, d.LABEL)
		}
		return fmt.Errorf("'%v' is not a valid distribution. Avaliable distributions: [%v]", wantedDistribution, strings.Join(avaliableDistributions, ","))
	}

	// find disks
	disks := make([]*disk, 0, 0)
	wantedDisks, err := s.Disks.Render(nil)
	if err != nil {
		return err
	}
	for _, part := range strings.Split(wantedDisks, ",") {
		subparts := strings.Split(strings.TrimSpace(part), ":")
		if len(subparts) != 2 {
			return fmt.Errorf("'%v' is not a valid disk declaration. Disks must be in the form type:size, with multiple disks seperated by comma. E.g.: 'ext:1024,swap:256'", part)
		}

		diskType := subparts[0]
		diskSizeStr := subparts[1]
		if diskType != "ext4" && diskType != "ext3" && diskType != "swap" && diskType != "raw" {
			return fmt.Errorf("'%v' is not a valid disk type. Valid disktypes: [ext4, ext3, swap, raw]", diskType)
		}

		diskSize, err := strconv.Atoi(diskSizeStr)
		if err != nil {
			return fmt.Errorf("%v is not a valid disk size: %v", diskSizeStr, err)
		}

		disks = append(disks, &disk{
			label:          fmt.Sprintf("%v%v", diskType, len(disks)+1),
			distributionID: -1,
			diskType:       diskType,
			size:           diskSize,
		})
	}

	// calculate disk sizes
	totalHD := plan.DISK * 1024
	diskSizeUsed := 0
	for _, disk := range disks {
		diskSizeUsed += disk.size
	}
	if totalHD-diskSizeUsed-dist.MINIMAGESIZE < 0 {
		return fmt.Errorf("The size of the distribution (%v) + the combined size of other disks (%v) exceeds the capacity of the machine (%v)", dist.MINIMAGESIZE, diskSizeUsed, totalHD)
	}
	disks = append([]*disk{&disk{
		label:          dist.LABEL,
		distributionID: dist.DISTRIBUTIONID,
		size:           totalHD - diskSizeUsed,
	}}, disks...)

	// create the linode
	l.Logf(" - creating linode")
	linodeID, err := a.client.linodeCreate(datacenterID, plan.PLANID)
	a.listServerCache = nil
	if err != nil {
		return err
	}

	// set the node label
	l.Logf(" - setting label to %v", label)
	err = a.client.linodeUpdateLabel(linodeID, label)
	if err != nil {
		return err
	}

	// create private ips for the machine
	if s.PrivateIPs < 0 || s.PrivateIPs > 3 {
		return fmt.Errorf("you can only have 0-3 private ips, not %v", s.PrivateIPs)
	}
	for i := 0; i < s.PrivateIPs; i++ {
		l.Logf(" - adding private ip #%v", i+1)
		err = a.client.linodeIPAddPrivate(linodeID)
		if err != nil {
			return err
		}
	}

	// create the disks.
	jobs := make([]createDiskJob, 0, 0)
	for _, disk := range disks {
		if disk.distributionID > 0 {
			l.Logf(" - creating os disk of size %v", disk.size)
			j, err := a.client.linodeDiskCreateFromDistribution(linodeID, disk.distributionID, disk.label, disk.size, rootPassword, rootSSHKey)
			if err != nil {
				return err
			}
			disk.id = j.DiskID
			jobs = append(jobs, j)
		} else {
			l.Logf(" - creating %v disk of size %v", disk.diskType, disk.size)
			j, err := a.client.linodeDiskCreate(linodeID, disk.label, disk.diskType, false, disk.size)
			if err != nil {
				return err
			}
			disk.id = j.DiskID
			jobs = append(jobs, j)
		}
	}

	// Wait for disks to be created
	if err := a.waitForJobsToFinish(linodeID, time.Minute*5, "   ", l); err != nil {
		return err
	}

	// create configuration for the machine
	disklist := ""
	for i, disk := range disks {
		if i > 0 {
			disklist += ","
		}
		disklist += fmt.Sprintf("%v", disk.id)
	}
	l.Logf(" - creating server configuration.")
	_, err = a.client.linodeConfigCreate(linodeID, kernelID, dist.LABEL, "", 0, disklist, "paravirt", "default", 1, "", false, true, true, true, true, true)
	if err != nil {
		return err
	}

	// boot the machine
	l.Logf(" - booting machine")
	err = a.client.linodeBoot(linodeID)
	if err != nil {
		return err
	}

	// wait for boot to complete
	if err := a.waitForJobsToFinish(linodeID, time.Minute*5, "   ", l); err != nil {
		return err
	}

	return nil
}

func (a *linodeAccount) waitForJobsToFinish(linodeID int, maxWait time.Duration, messagePrefix string, l schema.Logger) error {
	start := time.Now()
	lastUnfinished := 0
	for {
		jobs, err := a.client.linodeJobList(linodeID, true)
		if err != nil {
			return err
		}
		unfinished := 0
		names := make([]string, 0)
		for _, j := range jobs {
			if j.HOST_FINISH_DT == "" {
				names = append(names, j.ACTION)
				unfinished++
			}
		}

		if unfinished == 0 {
			break
		}

		if unfinished != lastUnfinished {
			if unfinished == 1 {
				l.Logf(messagePrefix+"waiting for 1 job to finish: %v", names[0])
			} else if unfinished > 1 {
				l.Logf(messagePrefix+"waiting for %v jobs to finish: %v", unfinished, strings.Join(names, ", "))
			}
			lastUnfinished = unfinished
		}

		time.Sleep(time.Second * 1)
		if time.Since(start) > maxWait {
			return fmt.Errorf("aborting after waiting %v for jobs to finish", time.Since(start))
		}
	}
	return nil
}

func planString(p *plan) string {
	return fmt.Sprintf("%v/%vcores/%vmb/%vgb/%vxfer", p.LABEL, p.CORES, p.RAM, p.DISK, p.XFER)
}
