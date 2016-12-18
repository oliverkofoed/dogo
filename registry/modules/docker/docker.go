package docker

import (
	"bytes"
	"context"
	"crypto/sha1"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/tlsconfig"
	"github.com/oliverkofoed/dogo/commandtree"
	"github.com/oliverkofoed/dogo/neaterror"
	"github.com/oliverkofoed/dogo/registry/modules/firewall"
	"github.com/oliverkofoed/dogo/registry/utilities"
	"github.com/oliverkofoed/dogo/schema"
	"github.com/oliverkofoed/dogo/snobgob"
)

var notInstalledErr = errors.New("not installed")

type Docker struct {
	// which image to run
	Folder schema.Template
	Image  schema.Template

	// how the container should be configured.
	Name    schema.Template `required:"yes" description:"The container name to use."`
	Command schema.Template
	Options []schema.Template
}

type state struct {
	Installed  bool
	Containers []types.Container
	Images     []types.ImageSummary
}

// Manager is the main entry point to this Dogo Module
var Manager = schema.ModuleManager{
	Name:            "docker",
	ModulePrototype: &Docker{},
	StatePrototype:  &state{},
	GobRegister: func() {
		snobgob.Register(&startDockerRegistryAndSSHTunnelCommand{})
		snobgob.Register(&dockerTagPushCommand{})
		snobgob.Register(&containerCommand{})
		snobgob.Register(&removeImagesCommand{})
		snobgob.Register(&installDockerCommand{})
	},
	GetState: func(query interface{}) (interface{}, error) {
		state := &state{Installed: true}

		// get a docker client
		client, err := getClient()
		if err != nil {
			if err == notInstalledErr {
				state.Installed = false
				return state, nil
			}

			return nil, fmt.Errorf("Could not get docker client from environment. Details: %v", err.Error())
		}

		// list containers
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		containers, err := client.ContainerList(ctx, types.ContainerListOptions{All: true})
		if err != nil {
			return nil, fmt.Errorf("Could not list Docker Containers. Error: %v", err)
		}
		state.Containers = containers

		// list installed images
		images, err := client.ImageList(context.Background(), types.ImageListOptions{All: true})
		if err != nil {
			return nil, fmt.Errorf("Could not list Docker Images. Error: %v", err.Error())
		}
		state.Images = images

		return state, nil
	},
	CalculateCommands: func(c *schema.CalculateCommandsArgs) error {
		remoteState := c.State.(*state)
		modules := c.Modules.([]*Docker)

		// nothing to do,
		if len(modules) == 0 && !remoteState.Installed {
			return nil
		}

		// figure out which images needs to be uploaded to remote system
		if len(modules) == 0 {
			return nil
		}

		// install docker if it's not installed.
		remoteRoot := c.RemoteCommands.AsCommand()
		if !remoteState.Installed {
			remoteRoot = remoteRoot.Add("Install Docker", &installDockerCommand{}).AsCommand()
		}

		// list local images
		client, err := getClient()
		var localImages []types.ImageSummary
		func() {
			// docker client will panic if the deamon isn't there, which is why this extra recover is required.
			defer func() {
				if r := recover(); r != nil {
					err = fmt.Errorf("Could not communicate with docker deamon. (details: panic: %v)", r)
				}
			}()
			localImages, err = client.ImageList(context.Background(), types.ImageListOptions{All: true})
		}()
		if err != nil {
			return neaterror.New(map[string]interface{}{
				"error message": err.Error(),
			}, "Could not list local Docker images. Is Docker installed and running?")
		}

		// local image map
		localImageMap := make(map[string]types.ImageSummary)
		localImageUsage := make(map[string]bool)
		for _, img := range localImages {
			localImageMap[img.ID] = img
			localImageUsage[img.ID] = false
		}

		// build remote image map
		remoteImageMap := make(map[string]types.ImageSummary)
		for _, img := range remoteState.Images {
			remoteImageMap[img.ID] = img
		}

		// figure out which images to push to registry
		rootPushCommand := &startDockerRegistryAndSSHTunnelCommand{connection: c.RemoteConnection}
		pushCommands := make(map[string]*commandtree.Command) // tag -> push command.
		containerNames := make(map[string]bool)
		for _, module := range modules {
			// find the tag for the image
			tag, err := module.Image.Render(nil)
			if err != nil {
				return err
			}
			folder, err := module.Folder.Render(nil)
			if err != nil {
				return err
			}
			if folder != "" {
				tag = filepath.Base(folder) + ":latest"
			}

			// find the corresponding image currently on the local machine.
			var localImage types.ImageSummary
			found := false
			for _, image := range localImages {
				for _, imageTag := range image.RepoTags {
					if imageTag == tag {
						localImage = image
						found = true
						break
					}
				}
			}
			if !found {
				return fmt.Errorf("Could not locate image tag '%v' on this machine. Are you sure it's built?", tag)
			}

			// mark usage of image
			l := localImage
			for true {
				localImageUsage[l.ID] = true

				if l.ParentID != "" {
					if parent, found := localImageMap[l.ParentID]; !found {
						return fmt.Errorf("Could not find image %v locally. This shouldn't ever happpen", l.ParentID)
					} else {
						l = parent
					}
				} else {
					break
				}
			}

			// check if we need to push it to registry.
			_, alreadyInRemote := remoteImageMap[localImage.ID]
			_, markedForPush := pushCommands[tag]
			if !alreadyInRemote && !markedForPush {
				pushCommands[tag] = rootPushCommand.AsCommand().Add("Push "+tag, &dockerTagPushCommand{
					client:  client,
					imageID: localImage.ID,
					tag:     tag,
				}).AsCommand()
				if len(pushCommands) == 1 {
					c.LocalCommands.Add("Push Docker Images", rootPushCommand)
				}
			}

			// figure out config for container
			command, err := module.Command.Render(nil)
			if err != nil {
				return err
			}
			options := make([]string, 0, len(module.Options))
			for _, t := range module.Options {
				opt, err := t.Render(nil)
				if err != nil {
					return err
				}
				options = append(options, opt)
			}

			// build the id.
			h := sha1.New()
			h.Write([]byte(command))
			h.Write([]byte(localImage.ID))
			h.Write([]byte(folder))
			for _, option := range options {
				h.Write([]byte(option))
			}
			containerVersion := fmt.Sprintf("%x", h.Sum(nil))

			// find the name for the container
			containerName, err := module.Name.Render(nil)
			if err != nil {
				return err
			}
			if containerName == "" {
				return fmt.Errorf("Container must have a name. ")
			}
			if _, found := containerNames[containerName]; found {
				return fmt.Errorf("the container name '%v' is used more than once", containerName)
			}
			containerNames[containerName] = true

			// do we need to start the container?
			startContainer := true
			stopID := ""

			for _, container := range remoteState.Containers {
				for _, n := range container.Names {
					if n == "/"+containerName {
						stopID = container.ID
						if value, found := container.Labels["dogo"]; found {
							if value == containerVersion {
								if container.State == "running" {
									startContainer = false
								} else {
									c.Logf("will start %v bacause its state is '%v'", containerName, container.State)
								}
							}
						}
					}
				}
			}

			if startContainer {
				pullTag := ""
				if !alreadyInRemote {
					pullTag = fmt.Sprintf("127.0.0.1:%v/%v", registryPort, tag)
				}

				cmd := bytes.NewBuffer(nil)
				cmd.WriteString("docker run")
				cmd.WriteString(" --detach")
				cmd.WriteString(" --label dogo=" + containerVersion)
				cmd.WriteString(" --name " + containerName)
				for _, opt := range options {
					cmd.WriteString(" ")
					cmd.WriteString(opt)
				}
				cmd.WriteString(" " + localImage.ID)
				cmd.WriteString(" " + command)
				remoteRoot.Add("Docker Container: "+containerName, &containerCommand{
					PullTag:         pullTag,
					StopContainerID: stopID,
					StartCommand:    cmd.String(),
				})
			}
		}

		// send command to remove unused images if any
		for _, img := range remoteState.Images {
			if used, found := localImageUsage[img.ID]; !found || !used {
				usedInContainer := false
				for _, container := range remoteState.Containers {
					if container.ImageID == img.ID {
						usedInContainer = true
						break
					}
				}
				if !usedInContainer {
					//remoteRoot.Add("Remove unused image: "+img.ID, &removeImagesCommand{Image: img.ID})
				}
			}
		}

		return nil
	},
}

type installDockerCommand struct {
	commandtree.Command
}

func (c *installDockerCommand) Execute() {
	installCommand := "curl -sSL https://get.docker.com/ | sh"
	c.Logf("Installing docker with '%v'", installCommand)

	cmd := exec.Command("/bin/bash", "-c", installCommand)
	cmd.Stdout = commandtree.NewLogFuncWriter(" - ", c.Logf)
	cmd.Stderr = commandtree.NewLogFuncWriter(" - ", c.Logf)
	err := utilities.MachineExclusive(cmd.Run)
	if err != nil {
		c.Err(err)
		return
	}

	// check if actually installed.
	if _, err = getClient(); err != nil {
		if err == notInstalledErr {
			c.Errf("It does not look like the install finished sucessfully.")
		} else {
			c.Errf("Could not get docker client from environment. Details: %v", err.Error())
		}
	}

	// persist any firewall rules
	firewall.PersistRules(c)
}

type startDockerRegistryAndSSHTunnelCommand struct {
	commandtree.Command
	connection schema.ServerConnection
}

func (c *startDockerRegistryAndSSHTunnelCommand) Execute() {
	// Ensure the DckerRegistry is started.
	err := StartDockerRegistry("error")
	if err != nil {
		c.Errf("Couldn't start local docker registry: %v", err)
		return
	}

	// ensure the SSH Tunnel is started
	_, err = c.connection.StartTunnel(registryPort, registryPort, true)
	if err != nil {
		c.Errf("Couldn't create tunnel from remote machine to local docker registry: %v", err)
		return
	}
}

type dockerTagPushCommand struct {
	commandtree.Command
	tag     string
	client  *client.Client
	imageID string
}

var pushLock = sync.RWMutex{}
var pushMap = make(map[string]*sync.Once)

func (c *dockerTagPushCommand) Execute() {
	pushTag := fmt.Sprintf("%v/%v", registryAddr, c.tag)

	// In inprocess cache to ensure that if deploying to
	// 20 servers, the local push only happens once.
	pushLock.Lock()
	o, found := pushMap[pushTag]
	if !found {
		o = &sync.Once{}
		pushMap[pushTag] = o
	}
	pushLock.Unlock()

	o.Do(func() {
		// tag image.
		c.Logf("docker tag %v %v", c.imageID, pushTag)
		err := commandtree.OSExec(c.AsCommand(), "", " - ", "docker", "tag", c.imageID, pushTag)
		if err != nil {
			c.Errf(err.Error())
			return
		}

		// push image.
		c.Logf("docker push %v", pushTag)
		err = commandtree.OSExec(c.AsCommand(), "", " - ", "docker", "push", pushTag)
		if err != nil {
			if len(c.LogArray) > 0 {
				last := c.LogArray[len(c.LogArray)-1]
				if last.Error != nil && strings.Contains(last.Error.Error(), "HTTPS") {
					err = fmt.Errorf("It seems the docker deamon is trying to talk HTTPS to %v, when it should be talking HTTP. Did you add %v as an insecure-registry? (full err: %v)", registryAddr, registryAddr, err)
				}
			}
			c.Errf(err.Error())
			return
		}
	})
}

type containerCommand struct {
	commandtree.Command
	PullTag         string // tag to pull, if "", don't pull
	StopContainerID string
	StartCommand    string
}

var pullOnceMap = make(map[string]*sync.Mutex)
var pullOnceMapMutex sync.Mutex

func (c *containerCommand) Execute() {
	// pull the tag.
	if c.PullTag != "" {
		pullOnceMapMutex.Lock()
		pull := false
		m, found := pullOnceMap[c.PullTag]
		if !found {
			m = &sync.Mutex{}
			pull = true
			pullOnceMap[c.PullTag] = m
		}
		pullOnceMapMutex.Unlock()
		m.Lock()
		if pull {
			c.Logf("Pulling docker image")
			err := commandtree.OSExec(c.AsCommand(), "", " - ", "docker", "pull", c.PullTag)
			if err != nil {
				c.Errf(err.Error())
				return
			}
		}
		m.Unlock()
	}

	// stop existing container (just force it)
	if c.StopContainerID != "" {
		c.Logf("Stopping/Removing existing container")
		err := commandtree.OSExec(c.AsCommand(), "", " - ", "docker", "stop", c.StopContainerID)
		if err != nil {
			c.Errf(err.Error())
			return
		}
		err = commandtree.OSExec(c.AsCommand(), "", " - ", "docker", "rm", c.StopContainerID)
		if err != nil {
			c.Errf(err.Error())
			return
		}
	}

	// start the new one.
	c.Logf("Starting container: " + c.StartCommand)
	err := commandtree.OSExec(c.AsCommand(), "", " - ", "/bin/bash", "-c", c.StartCommand)
	if err != nil {
		c.Errf(err.Error())
		return
	}
}

type removeImagesCommand struct {
	commandtree.Command
	Image string
}

func (c *removeImagesCommand) Execute() {
	err := commandtree.OSExec(c.AsCommand(), "", "", "docker", "rmi", c.Image)
	if err != nil {
		c.Errf(err.Error())
		return
	}
}

func getClient() (*client.Client, error) {
	transport := &http.Transport{
		Dial: (&net.Dialer{
			Timeout:   200 * time.Millisecond,
			KeepAlive: 30 * time.Second,
		}).Dial,
	}
	if dockerCertPath := os.Getenv("DOCKER_CERT_PATH"); dockerCertPath != "" {
		options := tlsconfig.Options{
			CAFile:             filepath.Join(dockerCertPath, "ca.pem"),
			CertFile:           filepath.Join(dockerCertPath, "cert.pem"),
			KeyFile:            filepath.Join(dockerCertPath, "key.pem"),
			InsecureSkipVerify: os.Getenv("DOCKER_TLS_VERIFY") == "",
		}
		tlsc, err := tlsconfig.Client(options)
		if err != nil {
			return nil, err
		}

		transport.TLSClientConfig = tlsc
	}

	host := os.Getenv("DOCKER_HOST")
	if host == "" {
		sock := "/var/run/docker.sock"

		_, err := os.Stat(sock)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, notInstalledErr
			}
			return nil, err
		}

		host = "unix://" + sock
	}

	version := os.Getenv("DOCKER_API_VERSION")
	if version == "" {
		version = client.DefaultVersion
	}

	if host == "unix:///var/run/docker.sock" {
		return client.NewClient(host, version, nil, nil)
	}
	return client.NewClient(host, version, &http.Client{Transport: transport}, nil)
}
