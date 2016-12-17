package docker_test

import (
	"testing"

	"github.com/oliverkofoed/dogo/registry/modules/docker"
	"github.com/oliverkofoed/dogo/schema"
	"github.com/oliverkofoed/dogo/testmodule"
)

var agentpath = "/usr/bin/dogoagent"

func TestDocker(t *testing.T) {
	return
	testmodule.TestModule(t, false, "../../../", docker.Manager, []*docker.Docker{
		&docker.Docker{
			Name:    testmodule.MockTemplate("memcached"),
			Image:   testmodule.MockTemplate("memcached:alpine"),
			Folder:  testmodule.MockTemplate(""),
			Command: testmodule.MockTemplate(""),
			Options: []schema.Template{
				testmodule.MockTemplate(""),
			},
		},
		&docker.Docker{
			Name:    testmodule.MockTemplate("webserver"),
			Image:   testmodule.MockTemplate(""),
			Folder:  testmodule.MockTemplate("/some/folder/webserver"),
			Command: testmodule.MockTemplate(""),
			Options: []schema.Template{
				testmodule.MockTemplate(""),
			},
		},
	})
}
