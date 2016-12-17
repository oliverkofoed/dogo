package testmodule

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/oliverkofoed/dogo/commandtree"
	"github.com/oliverkofoed/dogo/registry"
	"github.com/oliverkofoed/dogo/schema"
	"github.com/oliverkofoed/dogo/ssh"
)

func TestModule(t *testing.T, checkAndUpdateAgent bool, dogoRootDir string, manager schema.ModuleManager, modules interface{}) {
	// register gob types
	registry.GobRegister()

	// debug printers
	logf := func(format string, args ...interface{}) {
		fmt.Printf(format+"\n", args...)
	}
	errf := func(format string, args ...interface{}) {
		t.Errorf(format, args...)
		t.FailNow()
	}

	// connect to remote system
	logf("Connecting to remote system")
	path := "/Users/oliverkofoed/Dropbox/GoRoot/src/github.com/oliverkofoed/dogo/.dogocache/vagrant/box/.vagrant/machines/box/virtualbox/private_key"
	privatekey, err := ioutil.ReadFile(path)
	if err != nil {
		t.Error(err)
		t.FailNow()
	}

	connection, err := ssh.NewSSHConnection("127.0.0.1", 2200, "vagrant", "", privatekey, time.Second*30)
	if err != nil {
		errf(err.Error())
	}
	defer connection.Close()

	// build agent fast, check hash, and rebuild if required by remote system.
	if checkAndUpdateAgent {
		logf(" - checking if agent has changed.")
		cmd := exec.Command("testmodule/setagenthash.sh")
		cmd.Dir = dogoRootDir
		out, err := cmd.Output()
		if err != nil {
			errf(err.Error())
		}

		// check if we need to create and a new agent
		uploadAgent := false
		_, err = connection.ExecuteCommand("test -f " + schema.AgentPath)
		if err != nil {
			logf(" - agent not found on remote system.")
			uploadAgent = true
		}
		if strings.TrimSpace(string(out)) != agenthash {
			logf(" - agent changed. was: %v, now: %v", agenthash, strings.TrimSpace(string(out)))
			uploadAgent = true
		}

		if uploadAgent {
			// get remote os
			goos := "linux"
			uname, err := connection.ExecuteCommand("uname")
			if err == nil {
				if strings.Contains(uname, "Darwin") {
					goos = "darwin"
				}
			}

			// build new agent & upload.
			start := time.Now()
			print(" - building new agent...")
			cmd := exec.Command("/bin/bash", "-c", "GOOS="+goos+" GOARCH=amd64 CGO_ENABLED=0 go build -o .build/agent."+goos+" . ")
			cmd.Dir = dogoRootDir + "agent/"
			_, err = cmd.Output()
			if err != nil {
				errf(err.Error())
			}
			print(fmt.Sprintf(" took: %v\n", time.Since(start)))

			// delete preexisting agent file
			_, _ = connection.ExecuteCommand("rm " + schema.AgentPath)

			// upload agent
			logf(" - uploading new agent")
			agentBytes, err := ioutil.ReadFile(dogoRootDir + "agent/.build/agent." + goos)
			if err != nil {
				errf(err.Error())
			}
			err = connection.WriteFile(schema.AgentPath, 755, int64(len(agentBytes)), bytes.NewReader(agentBytes), true, func(p float64) {})
		}
	} else {
		logf("[WARN] NOT CHECKING IF AGENT IS VALID OR UP-TO-DATE. PROCEED AT YOUR OWN RISK.")
	}
	// get remote state
	logf("Get remote state")
	root := commandtree.NewRootCommand("Get Agent State")
	root.Add("get agent state", &registry.GetStateCommand{})
	err = connection.ExecutePipeCommand("sudo "+schema.AgentPath+" exec", func(reader io.Reader, errorReader io.Reader, writer io.Writer) error {
		start := time.Now()
		err := commandtree.StreamCall(root, root, 5, reader, errorReader, writer, func(s string) { logf(" - " + s) })
		duration := time.Since(start)
		if duration > time.Second {
			logf(" - warning: it took %v", duration)
		}
		return err
	})
	if err != nil {
		errf("bad" + err.Error())
	}

	// Dig out state from command.
	var remoteState *schema.ServerState
	for _, child := range root.Children {
		result := child.AsCommand().GetResult()
		if result != nil {
			if s, ok := result.(schema.ServerState); ok {
				remoteState = &s
			}
		}
	}

	// get remote state.
	if remoteState == nil {
		errf("Did not get result from dogoagent.")
	}

	remoteModuleState := remoteState.Modules[manager.Name]
	if e, ok := remoteModuleState.(error); ok {
		errf(e.Error())
	}
	if e, ok := remoteModuleState.(string); ok {
		errf(e)
	}

	// create reusable args object
	logf("Calculating commands to run on local and remote system")
	remote := commandtree.NewRootCommand("Remote Commands")
	args := schema.CalculateCommandsArgs{
		LocalCommands:    commandtree.NewRootCommand("Local Commands"),
		RemoteCommands:   remote,
		RemoteConnection: connection,
	}

	// calculate changes for each module
	start := time.Now()
	args.Logf = logf
	args.Errf = errf
	if manager.CalculateCommands == nil {
		panic(fmt.Sprintf("%v.CalculateCommands is nil. Should be a func.", manager.Name))
	}
	args.State = remoteModuleState
	args.Modules = modules
	err = manager.CalculateCommands(&args)
	if err != nil {
		errf(err.Error())
	}
	if time.Since(start) > time.Second {
		logf(" - warning: it took %v", time.Since(start))
	}

	// bail if nothing to do.
	if len(remote.Children) == 0 && len(args.LocalCommands.Children) == 0 {
		logf("No Command Generated.")
		return
	}

	// execute local commands
	if len(args.LocalCommands.Children) > 0 {
		logf("Executing local commands.")
		r := commandtree.NewRunner(args.LocalCommands, 1)
		allSuccess := false
		go func() {
			allSuccess = r.Run(nil)
			args.LocalCommands.State = commandtree.CommandStateCompleted
		}()
		commandtree.ConsoleUI(args.LocalCommands)
		if !allSuccess {
			errf("Err: not all local commands completed successfully")
			return
		}
	}

	// execute remote commands
	if len(remote.Children) > 0 {
		logf("Executing remote commands.")
		start = time.Now()
		root := commandtree.NewRootCommand("Remote Commands")
		err := connection.ExecutePipeCommand("sudo "+schema.AgentPath+" exec", func(reader io.Reader, errorReader io.Reader, writer io.Writer) error {
			start := time.Now()
			err := commandtree.StreamCall(remote, root, 5, reader, errorReader, writer, func(s string) { logf(" - " + s) })
			duration := time.Since(start)
			if duration > time.Second {
				logf(" - warning: it took %v\n", duration)
			}
			return err
		})
		if err != nil {
			errf(err.Error())
		}
		if time.Since(start) > time.Second {
			logf(" - warning: it took %v\n", time.Since(start))
		}
		root.State = commandtree.CommandStateCompleted
		commandtree.ConsoleUI(root)
	}
}

type MockTemplate string

func (m MockTemplate) Render(extra map[string]interface{}) (string, error) {
	return string(m), nil
}

func (m MockTemplate) RenderFile(extraArgs map[string]interface{}) (io.ReadCloser, int64, os.FileMode, error) {
	panic("MockTemplate does not implement RenderFile. Use MockFileTemplate instead.")
}

func (m MockTemplate) RenderFileBytes(extraArgs map[string]interface{}) ([]byte, error) {
	panic("MockTemplate does not implement RenderFileBytes. Use MockFileTemplate instead.")
}

type MockFileTemplate []byte

func (m MockFileTemplate) Render(extra map[string]interface{}) (string, error) {
	panic("MockFileTemplate does not implement Render. Use MockTemplate instead.")
}

func (m MockFileTemplate) RenderFile(extraArgs map[string]interface{}) (io.ReadCloser, int64, os.FileMode, error) {
	b := []byte(m)
	return &byteReadCloser{r: bytes.NewReader(b)}, int64(len(b)), 0, nil
}

func (m MockFileTemplate) RenderFileBytes(extraArgs map[string]interface{}) ([]byte, error) {
	return []byte(m), nil
}

type byteReadCloser struct {
	r io.Reader
}

func (b *byteReadCloser) Read(p []byte) (n int, err error) {
	return b.r.Read(p)
}

func (b *byteReadCloser) Close() error {
	return nil
}
