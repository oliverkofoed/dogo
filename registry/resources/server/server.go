package server

import (
	"time"

	"github.com/oliverkofoed/dogo/commandtree"
	"github.com/oliverkofoed/dogo/schema"
	"github.com/oliverkofoed/dogo/ssh"
)

type ServerGroup struct {
}

type Server struct {
	Name          string
	PublicIP      schema.Template `required:"true" defaultdescription:"The public ip to connect to the server with"`
	SSHPrivateKey schema.Template `required:"true" defaultdescription:"The private key used to authenticate against to the server"`
	SSHPublicKey  schema.Template `required:"true" defaultdescription:"The public key used to authenticate "`
	info          *machineInfo
}

type machineInfo struct {
	PublicIPs  []string
	PrivateIPs []string
}

func (s *Server) OpenConnection() (schema.ServerConnection, error) {
	privateKey, err := s.SSHPrivateKey.RenderFileBytes(nil)
	if err != nil {
		return nil, err
	}

	ip, err := s.PublicIP.Render(nil)
	if err != nil {
		return nil, err
	}

	return ssh.NewSSHConnection(ip, 22, "root", "", privateKey, time.Second*30)
}

func (g *ServerGroup) Validate() error {
	return nil
}

// Manager is the main entry point for this resource type
var Manager = schema.ResourceManager{
	Name:              "servers",
	ResourcePrototype: &Server{},
	GroupPrototype:    &ServerGroup{},
	Provision: func(group interface{}, resource interface{}, l schema.Logger) error {
		return nil
	},
	FindUnused: func(shouldExist map[interface{}][]string, decommisionRoot *commandtree.Command, l schema.Logger) ([]string, error) {
		return []string{}, nil
	},
}
