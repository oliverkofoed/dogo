package vagrant_test

import (
	"testing"

	"github.com/oliverkofoed/dogo/registry/resources/vagrant"
	"github.com/oliverkofoed/dogo/schema"
	"github.com/oliverkofoed/dogo/testmodule"
)

func TestVirtualBox(t *testing.T) {
	return
	l := &schema.ConsoleLogger{}
	group := &vagrant.VagrantGroup{DecommissionTag: "Flurry"}

	// create box1
	box1 := &vagrant.Vagrant{
		Name:          ("testvagrant1"),
		Box:           testmodule.MockTemplate("ubuntu/trusty64"),
		Memory:        2048,
		CPUs:          1,
		PrivateIPs:    []schema.Template{testmodule.MockTemplate("192.168.100.2")},
		SharedFolders: []schema.Template{},
	}
	err := vagrant.Manager.Provision(group, box1, l)
	if err != nil {
		t.Error(err)
		return
	}

	// create box2
	box2 := &vagrant.Vagrant{
		Name:          ("testvagrant2"),
		Box:           testmodule.MockTemplate("ubuntu/trusty64"),
		Memory:        2048,
		CPUs:          1,
		PrivateIPs:    []schema.Template{testmodule.MockTemplate("192.168.100.2")},
		SharedFolders: []schema.Template{},
	}
	err = vagrant.Manager.Provision(group, box2, l)
	if err != nil {
		t.Error(err)
		return
	}
}
