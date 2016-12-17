package registry

import (
	"errors"
	"fmt"
	"sync"

	"os"
	"runtime"

	"github.com/oliverkofoed/dogo/commandtree"
	"github.com/oliverkofoed/dogo/neaterror"
	"github.com/oliverkofoed/dogo/registry/modules/docker"
	"github.com/oliverkofoed/dogo/registry/modules/dogo"
	"github.com/oliverkofoed/dogo/registry/modules/file"
	"github.com/oliverkofoed/dogo/registry/modules/firewall"
	"github.com/oliverkofoed/dogo/registry/resources/linode"
	"github.com/oliverkofoed/dogo/registry/resources/localhost"
	"github.com/oliverkofoed/dogo/registry/resources/server"
	"github.com/oliverkofoed/dogo/registry/resources/vagrant"
	"github.com/oliverkofoed/dogo/schema"
	"github.com/oliverkofoed/dogo/snobgob"
	"github.com/oliverkofoed/dogo/version"
)

// ModuleManagers is the list of registered modules
var ModuleManagers = map[string]*schema.ModuleManager{
	docker.Manager.Name:   &docker.Manager,
	firewall.Manager.Name: &firewall.Manager,
	dogo.Manager.Name:     &dogo.Manager,
	file.Manager.Name:     &file.Manager,
}

// ResourceManagers is the list of registered resource providers
var ResourceManagers = map[string]*schema.ResourceManager{
	server.Manager.Name:    &server.Manager,
	vagrant.Manager.Name:   &vagrant.Manager,
	linode.Manager.Name:    &linode.Manager,
	localhost.Manager.Name: &localhost.Manager,
}

func GobRegister() {
	snobgob.Register(neaterror.Error{})
	snobgob.Register(commandtree.RootCommand{})
	snobgob.Register(GetStateCommand{})
	snobgob.Register(schema.ServerState{})
	snobgob.Register(errors.New("test"))
	snobgob.Register(commandtree.NewExecCommand("", "", "", "", ""))
	snobgob.Register(commandtree.NewBashCommands("", "", "", "", ""))
	snobgob.Register(DefaultStateQuery{})

	for _, m := range ModuleManagers {
		if m.ModulePrototype != nil {
			snobgob.Register(m.ModulePrototype)
		}
		if m.StatePrototype != nil {
			snobgob.Register(m.StatePrototype)
		}
		if m.GobRegister != nil {
			m.GobRegister()
		}
	}
}

type GetStateCommand struct {
	commandtree.Command
	Query map[string]interface{}
}

type DefaultStateQuery struct {
}

func (c *GetStateCommand) Execute() {
	state := &schema.ServerState{
		Version: version.Version,
		OS:      runtime.GOOS,
		UID:     os.Getuid(),
		Modules: make(map[string]interface{}),
	}

	// collect state from the requested modules
	var wg sync.WaitGroup
	var lock sync.RWMutex
	for name, query := range c.Query {
		manager := ModuleManagers[name]
		wg.Add(1)
		go func(name string, manager *schema.ModuleManager, query interface{}) {
			defer func() {
				if r := recover(); r != nil {
					lock.Lock()
					state.Modules[name] = fmt.Errorf("Panic: %v", r)
					lock.Unlock()
					wg.Done()
				}
			}()

			moduleState, err := manager.GetState(query)
			lock.Lock()
			if err != nil {
				if _, ok := err.(neaterror.Error); !ok {
					err = neaterror.New(nil, err.Error())
				}
				state.Modules[name] = err
			} else {
				state.Modules[name] = moduleState
			}
			lock.Unlock()
			wg.Done()
		}(name, manager, query)
	}
	wg.Wait()

	c.SetResult(state)
}
