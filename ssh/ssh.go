package ssh

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/oliverkofoed/dogo/neaterror"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

type SSHConnection struct {
	connection *ssh.Client
}

func NewSSHConnection(host string, port int, username string, password string, privatekey []byte, timeout time.Duration) (*SSHConnection, error) {
	authMethods := make([]ssh.AuthMethod, 0, 3)

	if password != "" {
		// authenticate with password?
		authMethods = append(authMethods, ssh.Password(password))
	} else if privatekey != nil {
		// authenticate with private key?
		key, err := ssh.ParsePrivateKey(privatekey)
		if err != nil {
			return nil, fmt.Errorf("Could not parse keyfile. Error: %v", err)
		}
		authMethods = append(authMethods, ssh.PublicKeys(key))
	} else {
		// authenticate with ssh-agent?
		if sshAgent, err := net.Dial("unix", os.Getenv("SSH_AUTH_SOCK")); err == nil {
			authMethods = append(authMethods, ssh.PublicKeysCallback(agent.NewClient(sshAgent).Signers))
			defer sshAgent.Close()
		}
	}

	// connect to server
	connection, err := ssh.Dial("tcp", fmt.Sprintf("%v:%v", host, port), &ssh.ClientConfig{
		User:    username,
		Auth:    authMethods,
		Timeout: timeout,
	})
	if err != nil {
		return nil, fmt.Errorf("Failed to connect to SSH on %v:%v. Error: %v", host, port, err)
	}

	return &SSHConnection{
		connection: connection,
	}, nil
}

func (s *SSHConnection) Close() error {
	return s.connection.Close()
}

func WaitForSSH(host string, port int, username string, password string, privatekey []byte, timeout time.Duration) error {
	start := time.Now()
	step := timeout / 2
	if step < time.Second {
		step = time.Second
	}

	var lastError error
	for time.Since(start) < timeout {
		conn, err := NewSSHConnection(host, port, username, password, privatekey, step)
		lastError = err
		if err != nil {
			if !strings.Contains(err.Error(), "Failed to connect to SSH on ") {
				return err
			}

			time.Sleep(time.Millisecond * 100)
		} else {
			conn.Close()
			return nil
		}
	}

	return fmt.Errorf("Could not establish an SSH connection connection in the given time %v. Last error: %v", timeout, lastError)
}

func (s *SSHConnection) Shell(stderr, stdout io.Writer, stdin io.Reader, width, height int) error {
	// create a session
	session, err := s.connection.NewSession()
	if err != nil {
		return fmt.Errorf("Failed to create SSH session: %v", err)
	}
	defer session.Close()

	// assign input/output
	session.Stdout = stdout
	session.Stderr = stderr
	session.Stdin = stdin

	// create pty
	err = session.RequestPty("xterm-256color", height, width, ssh.TerminalModes{
		ssh.ECHO:          1,     // enable echoing
		ssh.TTY_OP_ISPEED: 14400, // input speed = 14.4kbaud
		ssh.TTY_OP_OSPEED: 14400, // output speed = 14.4kbaud
	})
	if err != nil {
		return err
	}

	// run shell
	err = session.Shell()
	if err != nil {
		return err
	}
	return session.Wait()
}

func (s *SSHConnection) ExecutePipeCommand(command string, pipesFunc func(reader io.Reader, errorReader io.Reader, writer io.Writer) error) error {
	// create a session
	session, err := s.connection.NewSession()
	if err != nil {
		return fmt.Errorf("Failed to create SSH session: %v", err)
	}
	defer session.Close()

	// create writer
	writer, err := session.StdinPipe()
	defer writer.Close()
	if err != nil {
		return err
	}

	// create reader
	reader, err := session.StdoutPipe()
	if err != nil {
		return err
	}

	// create errorReader
	errorReader, err := session.StderrPipe()
	if err != nil {
		return err
	}

	// start
	runErr := session.Start(command)
	if runErr != nil {
		return errWrap(err, errorReader) //fmt.Errorf("Cmd.RunErr: %v", runErr)
	}

	// run pipesFunc
	pipesErr := pipesFunc(reader, errorReader, writer)
	if pipesErr != nil {
		return errWrap(pipesErr, errorReader)
	}

	waitErr := session.Wait()
	if waitErr != nil {
		return errWrap(waitErr, errorReader)
	}
	return nil
}

func errWrap(err error, errorReader io.Reader) error {
	b, err := ioutil.ReadAll(errorReader)
	if len(b) != 0 {
		return fmt.Errorf("%v: %v", err, string(b))
	}
	return err
}

func (s *SSHConnection) ExecuteCommand(command string) (string, error) {
	// create a session
	session, err := s.connection.NewSession()
	if err != nil {
		return "", fmt.Errorf("Failed to create SSH session: %v", err)
	}
	defer session.Close()

	// run command
	var buf bytes.Buffer
	session.Stdout = &buf
	session.Stderr = &buf
	if err := session.Run(command); err != nil {
		return "", fmt.Errorf("%v: %v", err, string(buf.Bytes()))
	}

	return buf.String(), nil
}

func (s *SSHConnection) WriteFile(path string, mode os.FileMode, contentLength int64, content io.Reader, sudo bool, progress func(p float64)) error {
	// create a session
	session, err := s.connection.NewSession()
	if err != nil {
		return fmt.Errorf("Failed to create SSH session: %v", err)
	}
	defer session.Close()

	// create writer
	writer, err := session.StdinPipe()
	if err != nil {
		return err
	}
	defer writer.Close()

	var buf bytes.Buffer
	session.Stdout = &buf
	session.Stderr = &buf

	// write data in seperate go routine
	go func() {
		fmt.Fprintln(writer, fmt.Sprintf("C%04d", int(mode)), contentLength, filepath.Base(path))

		arr := make([]byte, 32*1024)
		written := int64(0)
		for {
			nr, er := content.Read(arr)
			if nr > 0 {
				nw, ew := writer.Write(arr[0:nr])
				if nw > 0 {
					written += int64(nw)
				}
				if ew != nil {
					err = ew
					break
				}
				if nr != nw {
					err = io.ErrShortWrite
					break
				}
				p := float64(written) / float64(contentLength)
				progress(p)
			}
			if er == io.EOF {
				break
			}
			if er != nil {
				err = er
				break
			}
		}
		fmt.Fprint(writer, "\x00")
		writer.Close()
	}()

	command := "/usr/bin/scp -t " + filepath.Dir(path)
	if sudo {
		command = "sudo -n " + command
	}

	err = session.Run(command)
	if err != nil && buf.Len() != 0 {
		return fmt.Errorf("%v: %v", err, string(buf.Bytes()))
	}
	return err
}
func (s *SSHConnection) StartTunnel(localPort int, remotePort int, reverse bool) (listeningPort int, err error) {
	if reverse {
		if localPort == 0 {
			return 0, neaterror.New(nil, "the local port can't be %v for reverse tunnels. Must know what to connect to.", localPort)
		}
		return tunnel(fmt.Sprintf("127.0.0.1:%v", localPort), fmt.Sprintf("127.0.0.1:%v", remotePort), localPort, net.Dial, s.connection.Listen)
	} else {
		if remotePort == 0 {
			return 0, neaterror.New(nil, "the remote port can't be %v for tunnels. Must know what to connect to.", remotePort)
		}
		return tunnel(fmt.Sprintf("127.0.0.1:%v", remotePort), fmt.Sprintf("0.0.0.0:%v", localPort), remotePort, s.connection.Dial, net.Listen)
	}
	return 0, nil
}

func tunnel(dialAddress, listenAddress string, dialPort int, dial func(n, addr string) (net.Conn, error), listen func(n, addr string) (net.Listener, error)) (int, error) {
	// test address
	b, err := dial("tcp", dialAddress)
	if err != nil {
		if strings.Contains(err.Error(), "Connection refused") {
			return 0, fmt.Errorf("Could not connect to port %v when pre-testing the tunnel. Perhaps nothing is listening on the port?", dialPort)
		}
		return 0, err
	}
	b.Close()

	// start a listener
	listener, err := listen("tcp", listenAddress)
	if err != nil {
		return 0, err
	}

	// get the listening port used
	listeningPort := listener.Addr().(*net.TCPAddr).Port

	// accept and manage connections
	go func() {
		defer listener.Close()
		for {
			a, err := listener.Accept()
			if err != nil {
				return
			}

			b, err := dial("tcp", dialAddress)
			if err != nil {
				panic(err)
			}
			var wg sync.WaitGroup
			wg.Add(2)
			go func() {
				io.Copy(a, b)
				wg.Done()
			}()
			go func() {
				io.Copy(b, a)
				wg.Done()
			}()
			go func() {
				wg.Wait()
				a.Close()
				b.Close()
			}()
		}
	}()

	return listeningPort, nil
}

const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func GenerateRandomPassword(length int) (string, error) {
	b := make([]byte, length)

	n, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	if n != length {
		return "", fmt.Errorf("could not read %v random bytes for generating password", n)
	}

	for i := range b {
		b[i] = letterBytes[b[i]%byte(len(letterBytes))]
	}
	return string(b), nil
}
