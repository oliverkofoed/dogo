package virtualbox_test

import (
	"testing"

	"github.com/oliverkofoed/dogo/registry/resources/virtualbox"
	"github.com/oliverkofoed/dogo/schema"
	"github.com/oliverkofoed/dogo/testmodule"
)

func TestVirtualBox(t *testing.T) {
	l := &schema.ConsoleLogger{}
	group := &virtualbox.VirtualboxGroup{DecommissionTag: "test"}

	// create box1
	box1 := &virtualbox.Virtualbox{
		Name:       ("testvirtualbox"),
		Image:      ("https://cloud-images.ubuntu.com/releases/18.04/release/ubuntu-18.04-server-cloudimg-amd64.ova"),
		Memory:     2048,
		Storage:    20000,
		CPUs:       1,
		PrivateIPs: []schema.Template{testmodule.MockTemplate("192.168.100.2")},
		//SharedFolders: []schema.Template{},
	}
	err := virtualbox.Manager.Provision(group, box1, l)
	if err != nil {
		t.Error(err)
		return
	}
}
