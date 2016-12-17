package virtualbox

import (
	"fmt"
	"testing"

	"github.com/oliverkofoed/dogo/schema"
)

func TestVirtualBox(t *testing.T) {
	box := &virtualbox{
		Name:   "testvm1",
		Image:  "https://cloud-images.ubuntu.com/releases/16.04/release/ubuntu-16.04-server-cloudimg-amd64.ova",
		Memory: 1024,
		CPUs:   2,
	}

	err := box.Provision(&schema.ConsoleLogger{})
	if err != nil {
		t.Error(err)
		return
	}
	fmt.Println("done")
}
