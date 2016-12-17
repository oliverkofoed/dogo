package schema

import (
	"fmt"
	"io"
	"os"

	"github.com/oliverkofoed/dogo/commandtree"
)

const AgentPath = "/usr/local/bin/dogoagent"

type Config struct {
	Environments   map[string]*Environment
	Packages       map[string]*Package
	TemplateSource TemplateSource
}

type Environment struct {
	Name               string
	Vars               map[string]interface{}
	ManagerGroups      map[string][]interface{} // managername => groups
	Resources          map[string]*Resource
	ResourcesByPackage map[string][]*Resource
	DeploymentHooks    []*DeploymentHook
}

type DeploymentHook struct {
	RunBeforeDeployment bool
	RunAfterDeployment  bool
	CommandPackage      string
	CommandName         string
	Command             *Command
}

type Package struct {
	Name     string
	Tunnels  map[string]*Tunnel
	Commands map[string]*Command
	Modules  []*PackageModule
}

type PackageModule struct {
	ModuleName       string
	Config           map[string]interface{}
	OriginalLocation string
}

type Command struct {
	Local    bool       `description:"Should the command be executed on the machine running dogo? The default is false, which means run the command on the remote server"`
	Tunnels  []string   `description:"Tunnels required to execute this command. They'll be avalaible in the 'tunnels' template variable."`
	Commands []Template `required:"true" description:"The shell commands to run"`
	Target   Template   `description:"Which server(s) to run this command against. Valid values are either '' (the first server), '*' (all servers) or 'servername' (just that server)."`
}

type Tunnel struct {
	Port int `required:"true" description:"the local port to to use for the tunnel"`
}

type ServerConnection interface {
	Shell(stderr, stdout io.Writer, stdin io.Reader, width, height int) error
	ExecutePipeCommand(command string, pipesFunc func(reader io.Reader, errorReader io.Reader, writer io.Writer) error) error
	ExecuteCommand(command string) (string, error)
	WriteFile(path string, mode os.FileMode, contentLength int64, content io.Reader, sudo bool, progress func(float64)) error
	StartTunnel(localPort int, remotePort int, reverse bool) (listeningLocalPort int, err error)
	Close() error
}

type Resource struct {
	Name         string
	Manager      *ResourceManager
	ManagerGroup interface{}
	Resource     interface{}
	Packages     map[string]bool // packagename => used on this resource
	Data         map[string]interface{}
	Modules      map[string]interface{}
}

type ServerResource interface {
	OpenConnection() (ServerConnection, error)
}

// ServerState is the current state of several modules on a server
type ServerState struct {
	Version string
	OS      string
	UID     int
	Modules map[string]interface{}
}

// ModuleManager manages state for a piece of software on a server
type ModuleManager struct {
	Name                   string
	ModulePrototype        interface{}
	StatePrototype         interface{}
	CalculateGetStateQuery func(c *CalculateGetStateQueryArgs) (interface{}, error)
	GetState               func(query interface{}) (interface{}, error)
	CalculateCommands      func(c *CalculateCommandsArgs) error
	GobRegister            func()
}

type CalculateGetStateQueryArgs struct {
	Modules interface{}
	Logf    func(format string, args ...interface{})
	Errf    func(format string, args ...interface{})
	Err     func(err error)
}

type CalculateCommandsArgs struct {
	Modules          interface{}
	State            interface{}
	LocalCommands    *commandtree.RootCommand
	RemoteCommands   *commandtree.RootCommand
	RemoteConnection ServerConnection
	Environment      *Environment
	Config           *Config
	Logf             func(format string, args ...interface{})
	Errf             func(format string, args ...interface{})
	Err              func(err error)
}

type Template interface {
	Render(extraArgs map[string]interface{}) (string, error)
	RenderFile(extraArgs map[string]interface{}) (io.ReadCloser, int64, os.FileMode, error)
	RenderFileBytes(extraArgs map[string]interface{}) ([]byte, error)
}

type TemplateSource interface {
	NewTemplate(location string, template string, templateVars map[string]interface{}) (Template, error)
	AddGlobal(key string, value interface{})
}

type ResourceManager struct {
	Name              string
	GroupPrototype    interface{}
	ResourcePrototype interface{}
	Provision         func(group interface{}, resource interface{}, l Logger) error
	FindUnused        func(shouldExist map[interface{}][]string, decommisionRoot *commandtree.Command, l Logger) ([]string, error)
}

type Logger interface {
	Logf(format string, args ...interface{})
	Errf(format string, args ...interface{})
	Err(err error)
	SetProgress(progress float64)
}

type ConsoleLogger struct{}

func (l *ConsoleLogger) Logf(format string, args ...interface{}) {
	if len(args) == 0 {
		fmt.Println(format)
	} else {
		fmt.Println(fmt.Sprintf(format, args...))
	}
}
func (l *ConsoleLogger) Errf(format string, args ...interface{}) {
	if len(args) == 0 {
		fmt.Println("ERROR: " + format)
	} else {
		fmt.Println("ERROR: " + fmt.Sprintf(format, args...))
	}
}
func (l *ConsoleLogger) Err(err error) {
	fmt.Println("ERROR: " + err.Error())
}
func (l *ConsoleLogger) SetProgress(progress float64) {
	fmt.Println(fmt.Sprintf(" (%4.2f%%)", progress*100))
}

type PrefixLogger struct {
	Prefix string
	Output Logger
}

func (l *PrefixLogger) Logf(format string, args ...interface{}) {
	l.Output.Logf(l.Prefix+format, args...)
}
func (l *PrefixLogger) Errf(format string, args ...interface{}) {
	l.Output.Errf(l.Prefix+format, args...)
}
func (l *PrefixLogger) Err(err error) {
	l.Output.Errf(l.Prefix + "ERROR: " + err.Error())
}
func (l *PrefixLogger) SetProgress(progress float64) {
	l.Output.SetProgress(progress)
}
