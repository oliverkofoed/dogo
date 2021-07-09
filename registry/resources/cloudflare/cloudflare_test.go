package cloudflare_test

import (
	"os"
	"strings"
	"testing"

	"github.com/oliverkofoed/dogo/registry/resources/cloudflare"
	"github.com/oliverkofoed/dogo/schema"
	"github.com/oliverkofoed/dogo/testmodule"
)

func TestCloudflare(t *testing.T) {
	apikey, err := os.ReadFile("test_apikey.txt")
	if err != nil {
		t.Error(err)
		return
	}

	l := &schema.ConsoleLogger{}

	group := &cloudflare.Cloudflare{
		ZoneID:   testmodule.MockTemplate("b66b5b556cebca942245b9809ff1a5e6"),
		APIToken: testmodule.MockTemplate(string([]byte(strings.TrimSpace(string(apikey))))),
	}

	box1 := &cloudflare.DNS{
		Type: testmodule.MockTemplate("A"),
		//Name: testmodule.MockTemplate("stream[0-3].superhype.games."),
		//Name: testmodule.MockTemplate("stream[0-3].superhype.games"),
		Name:    testmodule.MockTemplate("stream[0-4].superhype.games"),
		Content: testmodule.MockTemplate("34.102.136.180,120.21.23.44"),
		TTL:     testmodule.MockTemplate("180"),
		Proxy:   false,
	}
	err = cloudflare.Manager.Provision(group, box1, l)
	if err != nil {
		t.Error(err)
		return
	}
}
