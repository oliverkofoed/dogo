package localhost

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"

	"github.com/oliverkofoed/dogo/schema"
)

// Manager is the main entry point for this resource type
var Manager = schema.ResourceManager{
	Name:              "localhost",
	ResourcePrototype: &Localhost{},
	GroupPrototype:    &Localhost{},
}

type Localhost struct {
}

func (s *Localhost) OpenConnection() (schema.ServerConnection, error) {
	return &localhostConnection{}, nil
}

type localhostConnection struct {
}

func (s *localhostConnection) Shell(cmdStr string, stderr, stdout io.Writer, stdin io.Reader, width, height int) error {
	// create a session
	/*session, err := s.connection.NewSession()
	if err != nil {
		return fmt.Errorf("Failed to create SSH session: %v", err)
	}
	defer session.Close()*/

	cmd := exec.Command("/bin/bash", "-c", cmdStr)

	// assign input/output
	cmd.Stderr = stderr
	cmd.Stdout = stdout
	cmd.Stdin = stdin

	// create pty
	//err = session.RequestPty("xterm-256color", height, width, ssh.TerminalModes{
	//ssh.ECHO:          1,     // enable echoing
	//ssh.TTY_OP_ISPEED: 14400, // input speed = 14.4kbaud
	//ssh.TTY_OP_OSPEED: 14400, // output speed = 14.4kbaud
	//})
	//if err != nil {
	//return err
	//}

	return cmd.Run()
}

func (c *localhostConnection) ExecutePipeCommand(command string, pipesFunc func(reader io.Reader, errorReader io.Reader, writer io.Writer) error) error {
	cmd := exec.Command("/bin/bash", "-c", command)

	// create writer
	writer, err := cmd.StdinPipe()
	defer writer.Close()
	if err != nil {
		return err
	}

	// create reader
	reader, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	// create errorReader
	errorReader, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	runErr := cmd.Start()
	if runErr != nil {
		return fmt.Errorf("Cmd.RunErr: %v", runErr)
	}

	pipesErr := pipesFunc(reader, errorReader, writer)
	if pipesErr != nil {
		return pipesErr
	}

	// check if there is anything left in the stderr
	b, err := ioutil.ReadAll(errorReader)
	if err != nil {
		return err
	} else if len(b) != 0 {
		return errors.New(string(b))
	}

	waitErr := cmd.Wait()
	if waitErr != nil {
		return fmt.Errorf("Cmd.WaitErr: %v", waitErr)
	}
	return nil
}

func (c *localhostConnection) ExecuteCommand(command string) (string, error) {
	var buf bytes.Buffer
	cmd := exec.Command("/bin/bash", "-c", command)
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
}

func (c *localhostConnection) WriteFile(path string, mode os.FileMode, contentLength int64, content io.Reader, sudo bool, progress func(float64)) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, content)
	if err != nil {
		return err
	}

	return err
}

func (c *localhostConnection) StartTunnel(localPort int, remotePort int, remoteHost string, reverse bool) (listeningLocalPort int, err error) {
	if remoteHost != "" && remoteHost != "127.0.0.1" {
		return 0, fmt.Errorf("Can't create localhost tunnel to %v", remoteHost)
	}
	if localPort == 0 {
		localPort = remotePort
	}
	return remotePort, nil
}

func (c *localhostConnection) Close() error {
	return nil
}
