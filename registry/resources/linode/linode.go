package linode

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"regexp"

	"github.com/linode/linodego"
	"github.com/oliverkofoed/dogo/commandtree"
	"github.com/oliverkofoed/dogo/schema"
	"github.com/oliverkofoed/dogo/snobgob"
	"github.com/oliverkofoed/dogo/ssh"
	"golang.org/x/oauth2"
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

//var nodeList []linode

// Manager is the main entry point for this resource type
var Manager = schema.ResourceManager{
	Name:              "linode",
	ResourcePrototype: &Linode{},
	GroupPrototype:    &LinodeGroup{},
	Provision: func(group interface{}, resource interface{}, l schema.Logger) error {
		ctx := context.Background()

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

		// create client
		client := linodego.NewClient(&http.Client{
			Transport: &oauth2.Transport{
				Source: oauth2.StaticTokenSource(&oauth2.Token{AccessToken: apikey}),
			},
		})
		client.SetRetryMaxWaitTime(time.Second * 60 * 15)
		client.SetRetryMaxWaitTime(time.Second * 20)
		//client.SetDebug(true)

		// get the manager for that account
		accountsLock.Lock()
		account, found := accounts[apikey]
		if !found {
			account = &linodeAccount{client: &client}
			accounts[apikey] = account
		}
		accountsLock.Unlock()

		// list servers.
		servers, err := account.listServers(l)
		if err != nil {
			return err
		}

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

			err = account.create(s, g.DecommissionTag, label, l, rootPassword, string(publicKey))
			if err != nil {
				l.Errf("Could not create server %v: %v", s.Name, err)

				// remove the node that was created, if any
				if nodes, err := account.listServers(l); err == nil {
					for _, node := range nodes {
						if node.Label == label {
							l.Logf("Trying to remove half-backed server %v", node.Label)
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
		ips, err := account.client.GetInstanceIPAddresses(ctx, server.ID)
		//ips, err := account.client.lindeIPList(server.LINODEID)
		if err != nil {
			return fmt.Errorf("Could not get ip for server %v: %v", s.Name, err)
		}
		for _, ip := range ips.IPv4.Public {
			info.PublicIPs = append(info.PublicIPs, ip.Address)
		}
		for _, ip := range ips.IPv4.Private {
			info.PrivateIPs = append(info.PrivateIPs, ip.Address)
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

				// create client
				client := linodego.NewClient(&http.Client{
					Transport: &oauth2.Transport{
						Source: oauth2.StaticTokenSource(&oauth2.Token{AccessToken: apikey}),
					},
				})

				// get the manager for that account
				accountsLock.Lock()
				account, found := accounts[apikey]
				if !found {
					account = &linodeAccount{client: &client}
					accounts[apikey] = account
				}
				accountsLock.Unlock()

				// list servers in that account
				servers, err := account.listServers(l)
				if err != nil {
					return nil, err
				}

				for _, node := range servers {
					if strings.HasPrefix(node.Label, group.DecommissionTag+"_") {
						found := false
						for _, name := range names {
							if node.Label == group.DecommissionTag+"_"+name {
								found = true
								break
							}
						}

						if !found {
							unusedNames = append(unusedNames, node.Label)
							decommisionRoot.Add("Decommission "+node.Label, &decommisionCommand{account: account, node: node})
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
	node    linodego.Instance
}

func (c *decommisionCommand) Execute() {
	err := c.account.decommissionServer(c.node, c)
	if err != nil {
		c.Err(err)
	}
}

type linodeAccount struct {
	sync.RWMutex
	client          *linodego.Client
	listServerCache map[string]linodego.Instance
}

func (a *linodeAccount) decommissionServer(node linodego.Instance, l schema.Logger) error {
	l.Logf("deleting instance %v (%v)", node.Label, node.ID)
	return a.client.DeleteInstance(context.Background(), node.ID)
}

func (a *linodeAccount) listServers(l schema.Logger) (map[string]linodego.Instance, error) {
	ctx := context.Background()

	a.Lock()
	defer a.Unlock()

	if a.listServerCache != nil {
		return a.listServerCache, nil
	}

	list, err := a.client.ListInstances(ctx, nil)
	if err != nil {
		return nil, err
	}

	m := make(map[string]linodego.Instance)
	for _, node := range list {
		m[node.Label] = node
	}
	a.listServerCache = m
	return a.listServerCache, nil
}

var infoOnce sync.Once
var initError error
var datacenters []linodego.Region
var distributions []linodego.Image
var kernels []linodego.LinodeKernel
var plans []linodego.LinodeType

func (a *linodeAccount) create(s *Linode, decomissionsTag string, label string, l schema.Logger, rootPassword string, rootSSHKey string) error {
	ctx := context.Background()

	// load initial data in parallel.
	infoOnce.Do(func() {
		var wg sync.WaitGroup
		wg.Add(4)
		go func() {
			if d, err := a.client.ListRegions(ctx, nil); err == nil {
				datacenters = d
			} else {
				initError = err
			}
			wg.Done()
		}()
		go func() {
			if p, err := a.client.ListTypes(ctx, nil); err == nil {
				plans = p
			} else {
				initError = err
			}
			wg.Done()
		}()
		go func() {
			if p, err := a.client.ListImages(ctx, nil); err == nil {
				distributions = p
			} else {
				initError = err
			}
			wg.Done()
		}()
		go func() {
			if d, err := a.client.ListKernels(ctx, nil); err == nil {
				kernels = d
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
	datacenterID := ""
	wantedDataCenter, err := s.Datacenter.Render(nil)
	if err != nil {
		return err
	}
	for _, dc := range datacenters {
		if dc.ID == wantedDataCenter {
			datacenterID = dc.ID
		}
	}
	if datacenterID == "" {
		avaliableDataCenters := make([]string, 0, len(datacenters))
		for _, dc := range datacenters {
			avaliableDataCenters = append(avaliableDataCenters, fmt.Sprintf("\"%v\" (%v)", dc.ID, dc.Country))
		}
		return fmt.Errorf("'%v' is not a valid datacenter. Avaliable data centers: [%v]", wantedDataCenter, strings.Join(avaliableDataCenters, ","))
	}

	// find plan
	var plan *linodego.LinodeType
	wantedPlan, err := s.Plan.Render(nil)
	if err != nil {
		return err
	}
	for _, p := range plans {
		if p.Label == wantedPlan {
			plan = &p
			break
		}
	}
	if plan == nil {
		avaliablePlans := make([]string, 0, len(plans))
		for _, p := range plans {
			avaliablePlans = append(avaliablePlans, fmt.Sprintf(" - \"%v\" (ram:%v, transfer:%v, price:$%v)", p.Label, p.Memory, p.Transfer, p.Price.Monthly))
		}
		return fmt.Errorf("'%v' is not a valid plan. Avaliable plans: \n%v", wantedPlan, strings.Join(avaliablePlans, ",\n"))
	}

	// find kernel
	var kernel linodego.LinodeKernel
	wantedKernel, err := s.Kernel.Render(nil)
	if err != nil {
		return err
	}
	avaliableKernels := make([]string, 0, len(kernels))
	for _, k := range kernels {
		avaliableKernels = append(avaliableKernels, fmt.Sprintf(" - \"%v\" (arch: %v, kvm: %v, xen:%v, version:%v)", k.Label, k.Architecture, k.KVM, k.XEN, k.Version))
	}
	for _, k := range kernels {
		if strings.HasPrefix(k.Label, wantedKernel) {
			if kernel.ID != "" {
				return fmt.Errorf("Multiple kernals match the prefix: '%v', please be more specific. Available kernels: \n%v", wantedKernel, strings.Join(avaliableKernels, "\n"))
			}
			kernel = k
		}
	}
	if kernel.ID == "" {
		return fmt.Errorf("'%v' is not a valid kernel. Avaliable kernels: \n%v", wantedKernel, strings.Join(avaliableKernels, "\n"))
	}

	// find distribution
	var dist linodego.Image
	wantedDistribution, err := s.Distribution.Render(nil)
	if err != nil {
		return err
	}
	for _, d := range distributions {
		if d.Label == wantedDistribution {
			dist = d
		}
	}
	if dist.ID == "" {
		avaliableDistributions := make([]string, 0, len(distributions))
		for _, d := range distributions {
			avaliableDistributions = append(avaliableDistributions, fmt.Sprintf(" - \"%v\" (%v, %vmb)", d.Label, d.Vendor, d.Size))
		}
		return fmt.Errorf("'%v' is not a valid distribution. Avaliable distributions: \n%v", wantedDistribution, strings.Join(avaliableDistributions, "\n"))
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
			distributionID: "",
			diskType:       diskType,
			size:           diskSize,
		})
	}

	// calculate disk sizes
	totalHD := plan.Disk
	diskSizeUsed := 0
	for _, disk := range disks {
		diskSizeUsed += disk.size
	}
	disks = append([]*disk{{
		label:          dist.Label,
		distributionID: dist.ID,
		size:           totalHD - diskSizeUsed,
	}}, disks...)

	// create instance
	l.Logf(" - creating instance %v", label)
	booted := false
	createOptions := linodego.InstanceCreateOptions{
		Region:         datacenterID,
		Type:           plan.ID,
		Label:          label,
		Group:          decomissionsTag,
		RootPass:       rootPassword,
		AuthorizedKeys: []string{rootSSHKey},
		Booted:         &booted,
	}
	a.listServerCache = nil
	instance, err := a.client.CreateInstance(ctx, createOptions)
	if err != nil {
		return err
	}

	// create private ips for the machine
	if s.PrivateIPs < 0 || s.PrivateIPs > 3 {
		return fmt.Errorf("you can only have 0-3 private ips, not %v", s.PrivateIPs)
	}
	for i := 0; i < s.PrivateIPs; i++ {
		l.Logf(" - adding private ip #%v", i+1)
		_, err = a.client.AddInstanceIPAddress(ctx, instance.ID, false)
		if err != nil {
			return err
		}
	}

	// create the disks.
	for _, disk := range disks {
		createOptions := linodego.InstanceDiskCreateOptions{
			Label:      disk.label,
			Size:       disk.size,
			Filesystem: disk.diskType,
		}
		if disk.distributionID != "" {
			l.Logf(" - creating os disk of size %v", disk.size)
			createOptions.Image = disk.distributionID
			createOptions.RootPass = rootPassword
			createOptions.AuthorizedKeys = []string{rootSSHKey}
		} else {
			l.Logf(" - creating %v disk of size %v", disk.diskType, disk.size)
		}
		j, err := a.client.CreateInstanceDisk(ctx, instance.ID, createOptions)
		if err != nil {
			return err
		}
		disk.id = j.ID
	}

	// create configuration for the machine
	devices := linodego.InstanceConfigDeviceMap{}
	for i, disk := range disks {
		switch i {
		case 0:
			devices.SDA = &linodego.InstanceConfigDevice{DiskID: disk.id}
			break
		case 1:
			devices.SDB = &linodego.InstanceConfigDevice{DiskID: disk.id}
			break
		case 2:
			devices.SDC = &linodego.InstanceConfigDevice{DiskID: disk.id}
			break
		case 3:
			devices.SDD = &linodego.InstanceConfigDevice{DiskID: disk.id}
			break
		case 4:
			devices.SDE = &linodego.InstanceConfigDevice{DiskID: disk.id}
			break
		case 5:
			devices.SDF = &linodego.InstanceConfigDevice{DiskID: disk.id}
			break
		}
	}

	// create configuration
	l.Logf(" - creating server configuration.")
	_, err = a.client.CreateInstanceConfig(ctx, instance.ID, linodego.InstanceConfigCreateOptions{
		Label:    dist.ID,
		Comments: "",
		Devices:  devices,
		Helpers: &linodego.InstanceConfigHelpers{
			UpdateDBDisabled:  true,
			Distro:            true,
			ModulesDep:        true,
			Network:           true,
			DevTmpFsAutomount: true,
		},
		MemoryLimit: 0,
		Kernel:      kernel.ID,
		RunLevel:    "default",
		VirtMode:    "paravirt",
	})
	if err != nil {
		return err
	}

	// boot instance
	l.Logf(" - booting instance.")
	err = a.client.BootInstance(ctx, instance.ID, 0)
	if err != nil {
		return err
	}

	end := time.Now().Add(time.Second * 60)
	for time.Now().Before(end) {
		time.Sleep(time.Second * 5)
		ns, err := a.client.GetInstance(ctx, instance.ID)
		if err != nil {
			return err
		}

		if ns.Status != linodego.InstanceRunning {
			l.Logf("   -> status:" + string(ns.Status))
		} else {
			return nil
		}
	}
	return fmt.Errorf("timed out wating for device to boot.")
}

type disk struct {
	id             int
	label          string
	distributionID string
	diskType       string
	size           int
}
