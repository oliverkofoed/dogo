package server

import (
	"time"

	"github.com/oliverkofoed/dogo/schema"
	"github.com/oliverkofoed/dogo/ssh"
)

// Manager is the main entry point for this resource type
var Manager = schema.ResourceManager{
	Name:              "server",
	ResourcePrototype: &server{},
	GroupPrototype:    &server{},
}

type server struct {
	Address string `required:"true" description:"the address of the server to SSH to."`
}

func (s *server) OpenConnection() (schema.ServerConnection, error) {
	return ssh.NewSSHConnection(s.Address, 22, "root", "", nil, time.Second*30)
}
