package qemu

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"time"

	"github.com/oliverkofoed/dogo/registry/utilities"
	"github.com/oliverkofoed/dogo/schema"
	"github.com/oliverkofoed/dogo/ssh"
)

// TODO: decomission unused servers (FindUnused)
// TODO: Socket/Bridge/Tap networking. (ability for the qemu servers to reach each other via bridge/tap networking)
// --> https://gist.github.com/mcastelino/88195a7d99811a177f5e643d1465e19e
// notes: https://www.dzombak.com/files/qemu-bridging-mavericks.pdf
// notes: https://powersj.io/posts/ubuntu-qemu-cli/
// notes: https://www.unixmen.com/qemu-kvm-using-copy-write-mode/

type Qemu struct {
	Name        string
	System      schema.Template `required:"true" default:"" description:"What kind of system are we doing"`
	Image       schema.Template `required:"true" default:"https://cloud-images.ubuntu.com/focal/current/focal-server-cloudimg-amd64.img" description:"path to a cloud image to use (.qcow2/.img)."`
	Memory      int             `required:"false" description:"The amount of ram to dedicate to the virtualjmachine"`
	CPUs        int             `required:"false" description:"The number of CPUs in the virtual machine"`
	Storage     int             `required:"false" description:"The size in mb of the storage in the virtual machine"`
	ShowDisplay bool            `required:"false" description:"Show the display or not"`
	info        *machineInfo
}

type machineInfo struct {
	SSHPort       int
	SSHPrivateKey []byte
}

func (s *Qemu) OpenConnection() (schema.ServerConnection, error) {
	if s.info == nil {
		return nil, errors.New("qemu servers must be provisioned before every use")
	}

	return ssh.NewSSHConnection("0.0.0.0", s.info.SSHPort, "root", "", s.info.SSHPrivateKey, time.Second*30)
}

type QemuGroup struct {
}

var boxLock = sync.RWMutex{}
var boxMap = make(map[string]*sync.Once)
var boxMapInitialized = false

// Manager is the main entry point for this resource type
var Manager = schema.ResourceManager{
	Name:              "qemu",
	ResourcePrototype: &Qemu{},
	GroupPrototype:    &QemuGroup{},
	Provision: func(group interface{}, resource interface{}, l schema.Logger) error {
		var err error
		//g := group.(*QemuGroup)
		s := resource.(*Qemu)

		// find the server label
		label := s.Name

		// paths
		metaDataPath := fmt.Sprintf(".dogocache/qemu/vm/%v/meta-data", label)
		userDataPath := fmt.Sprintf(".dogocache/qemu/vm/%v/user-data", label)
		ciDataPath := fmt.Sprintf(".dogocache/qemu/vm/%v/cidata.iso", label)
		hdaPath := fmt.Sprintf(".dogocache/qemu/vm/%v/disk.img", label)
		sshSuccess := fmt.Sprintf(".dogocache/qemu/vm/%v/sshsuccess", label)
		if err := os.MkdirAll(filepath.Dir(metaDataPath), os.ModePerm); err != nil {
			return err

		}

		// find an open port
		openPort := getOpenPort()

		// figure out the system
		system, err := s.System.Render(nil)
		if err != nil {
			return err
		}

		// build system args
		bin := ""
		args := []string{
			"-m", fmt.Sprintf("%v", s.Memory),
			"-smp", fmt.Sprintf("%v", s.CPUs),
			"-rtc", "base=localtime",
			"-name", label,
		}
		if !s.ShowDisplay {
			args = append(args,
				"-vga", "none",
				"-nographic",
			)
		}
		switch system {
		case "x86_64":
			bin, err = exec.LookPath("qemu-system-x86_64")
			if err != nil {
				return err
			}
			args = append(args,
				"-nodefaults",
				//"-chardev", "stdio,id=term0", // debug
				//"-serial", "chardev:term0", // debug
				"-drive", "if=virtio,format=qcow2,file="+hdaPath,
				"-drive", "if=virtio,format=raw,file="+ciDataPath,
				"-device", "e1000,netdev=net0",
				"-netdev", fmt.Sprintf("user,id=net0,hostfwd=tcp::%v-:22", openPort),

				// work-in-progress: use "-netdev socket" setup so machines can communicate with each other
				//"-device", "virtio-net,netdev=vlan",
				//"-netdev", "socket,id=vlan,mcast=230.0.0.1:1234",
			)
		case "aarch64":
			bin, err = exec.LookPath("qemu-system-aarch64")
			if err != nil {
				return err
			}

			// find qemu share folder
			searchDir := filepath.Dir(filepath.Dir(bin))
			edk2Aarch64Code := findFile(searchDir, "edk2-aarch64-code.fd")
			if edk2Aarch64Code == "" {
				return fmt.Errorf("Could not locate 'edk2-aarch64-code.fd' in folder: %v", searchDir)
			}

			//qemu-system-aarch64
			args = append(args,
				"-nodefaults",
				//"-chardev", "stdio,id=term0", // debug
				//"-serial", "chardev:term0", // debug
				"-cpu", "host",
				"-machine", "virt,highmem=off",
				"-accel", "hvf",
				"-accel", "tcg,tb-size=256",
				"-drive", "if=pflash,format=raw,unit=0,file="+edk2Aarch64Code+",readonly=on",

				// hda
				"-device", "virtio-blk-pci,drive=drive0,bootindex=0",
				"-drive", "if=none,media=disk,id=drive0,file="+hdaPath+",cache=writethrough",

				// networking
				"-device", "rtl8139,netdev=net0",
				"-netdev", fmt.Sprintf("user,id=net0,hostfwd=tcp::%v-:22", openPort),
			)

			// add cloud init if we haven't successfully connected once.
			if _, err := os.Stat(sshSuccess); errors.Is(err, os.ErrNotExist) {
				args = append(args,
					// cloud init drive
					"-device", "virtio-blk-pci,drive=drive1",
					"-drive", "if=none,media=disk,format=raw,id=drive1,file="+ciDataPath+",cache=writethrough",
				)
			}
		default:
			return fmt.Errorf("Unknown system: %v", system)
		}

		// ssh settings
		privateKey, publicKey, err := getSSH(ciDataPath)
		if err != nil {
			return err
		}

		// bail out if already running. (ps aux)
		if stdout, _, err := runCommand("ps", "aux"); err == nil {
			for _, line := range strings.Split(stdout, "\n") {
				if strings.Contains(line, hdaPath) {
					// found qemu
					pid := strings.FieldsFunc(line, func(c rune) bool {
						return c == ' '
					})[1]
					l.Logf("Qemu VM running in process with PID: %v", pid)

					// parse the port
					portEnd := strings.Index(line, "-:22")
					portStart := strings.LastIndex(line[:portEnd], ":")
					port, err := strconv.ParseInt(line[portStart+1:portEnd], 10, 64)
					if err != nil {
						fmt.Println("could not parse the port", err)
						runCommand("kill", "-9", pid)
						break
					}

					// try to connect and get info from machine
					info, err := waitForMachine(l, "0.0.0.0", int(port), privateKey, time.Second, sshSuccess)
					if err != nil {
						fmt.Println("could not ssh", err)
						return fmt.Errorf("Could not establish ssh connection to vm: %w", err)
					}

					// all good return
					s.info = info
					return nil
				}
			}
		}

		// find the public ssh key
		imgPath, err := s.Image.Render(nil)
		if err != nil {
			return err
		}
		if _, err := url.Parse(imgPath); err == nil {
			// calculate image name in cache.
			h := sha256.New()
			io.WriteString(h, imgPath)
			filename := filepath.Base(imgPath)
			imgUrl := imgPath
			imgPath = fmt.Sprintf(".dogocache/qemu/img/%x-%v", h.Sum(nil), filename)

			// download image if not in cache
			err := utilities.MachineExclusive(func() error {
				if _, err = os.Stat(imgPath); os.IsNotExist(err) {
					l.Logf("Downloading image: %v", imgUrl)

					//TODO: Ensure that only one download of this file happens at a time. (lock!)

					// start the download
					response, err := http.Get(imgUrl)
					if err != nil {
						return err
					}
					defer response.Body.Close()

					// create file to receive download
					downloadpath := imgPath + ".dl"
					err = os.MkdirAll(filepath.Dir(downloadpath), 0777)
					if err != nil {
						return err
					}
					f, err := os.Create(downloadpath)
					if err != nil {
						return err
					}
					defer f.Close()

					// process download
					reader := &progressReader{reader: response.Body, length: response.ContentLength}
					go func() {
						for reader.progress < 1 {
							l.SetProgress(reader.progress)
							time.Sleep(time.Second)
						}
						l.SetProgress(0)
					}()
					_, err = io.Copy(f, reader)
					if err != nil {
						return err
					}

					// download complete, rename!
					err = os.Rename(downloadpath, imgPath)
					if err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				return err
			}
		} else {
			if _, err := os.Stat(imgPath); os.IsNotExist(err) {
				return fmt.Errorf("could not find image at %v", imgPath)
			}
		}

		// create image if needed
		if _, err := os.Stat(hdaPath); os.IsNotExist(err) {
			if _, _, err := runCommand("cp", imgPath, hdaPath); err != nil {
				return err
			}
		}

		// resize image
		if _, _, err := runCommand("qemu-img", "resize", hdaPath, fmt.Sprintf("%vM", s.Storage)); err != nil {
			return err
		}

		// create the cloudinit metadata
		err = os.WriteFile(metaDataPath, []byte(fmt.Sprintf("instance-id: %v\nlocal-hostname: %v", s.Name, s.Name)), os.ModePerm)
		if err != nil {
			return err
		}

		// create the cloudinit userdata
		cloudConfig := "#cloud-config\nusers:\n  - name: root\n"
		if publicKey != nil && len(publicKey) > 0 {
			cloudConfig += "    ssh_authorized_keys:\n"
			cloudConfig += fmt.Sprintf("      - '%v'\n", strings.TrimSpace(string(publicKey)))
		}
		/*if rootPasswd != "" {
			cloudConfig += "    lock_passwd: false\n"
			cloudConfig += fmt.Sprintf("    plain_text_passwd: '%v'\n", string(rootPasswd))
		}*/
		err = os.WriteFile(userDataPath, []byte(cloudConfig), os.ModePerm)
		if err != nil {
			return err
		}

		// create the cidata.iso
		os.Remove(ciDataPath)
		runCommand("mkisofs", "-output", ciDataPath, "-volid", "cidata", "-joliet", "-rock", userDataPath, metaDataPath)
		if err != nil {
			return err
		}

		// boot the system
		if _, err := os.Stat(bin); os.IsNotExist(err) || bin == "" {
			return fmt.Errorf("Could not locate %v on this system", bin)
		}

		// start the vm
		l.Logf("Qemu command: %v %v", bin, strings.Join(args, " "))
		l.Logf("Starting vm... might take a bit...")
		cmd := exec.Command(bin, args...)
		cmd.Args[0] = "qemu_" + s.Name
		cmd.Stderr = &liveWriter{} // FOR DEBUG
		cmd.Stdout = &liveWriter{} // FOR DEBUG
		err = cmd.Start()
		if err != nil {
			return fmt.Errorf("Could not start VM: %v", err)
		}

		// wait for machine to boot and come up
		info, err := waitForMachine(l, "0.0.0.0", openPort, privateKey, time.Second*60*5, sshSuccess)
		if err != nil {
			cmd.Process.Kill()
			return err
		} else {
			s.info = info
			return nil
		}
	},
}

func getSSH(path string) ([]byte, []byte, error) { // private, public, error
	dir := filepath.Dir(path)

	// try reading files if they already exist
	privateKey, e1 := os.ReadFile(filepath.Join(dir, "id_rsa"))
	publicKey, e2 := os.ReadFile(filepath.Join(dir, "id_rsa.pub"))
	if e1 == nil && e2 == nil {
		return privateKey, publicKey, nil
	}

	// generate keys
	stdOut, stdErr, ex := runCommand("ssh-keygen", "-b", "2048", "-t", "rsa", "-f", filepath.Join(dir, "id_rsa"), "-P", "")
	if ex != nil {
		return nil, nil, fmt.Errorf("Could not generate ssh-key: %w. StdOut: %v, StdErr:%v", ex, stdOut, stdErr)
	}

	// read generated files
	privateKey, err := os.ReadFile(filepath.Join(dir, "id_rsa"))
	if err != nil {
		return nil, nil, err
	}
	publicKey, err = os.ReadFile(filepath.Join(dir, "id_rsa.pub"))
	if err != nil {
		return nil, nil, err
	}
	return privateKey, publicKey, nil
}

func waitForMachine(l schema.Logger, host string, port int, privateKey []byte, timeout time.Duration, sshSuccess string) (*machineInfo, error) {
	var lastErr error
	start := time.Now()
	for time.Since(start) < timeout {
		conn, err := ssh.NewSSHConnection(host, port, "root", "", privateKey, time.Second)
		if err != nil {
			lastErr = err
			if !strings.Contains(err.Error(), "Failed to connect to SSH on ") {
				return nil, err
			}

			time.Sleep(time.Millisecond * 100)
		} else {

			// create sshsuccessfil
			if _, err := os.Stat(sshSuccess); err != nil && os.IsNotExist(err) {
				file, _ := os.Create(sshSuccess)
				defer file.Close()
			}
			// connected, awesome
			conn.Close()
			return &machineInfo{SSHPort: port, SSHPrivateKey: privateKey}, nil
		}
	}
	return nil, fmt.Errorf("Could not establish connection to qemu vm in time. Something probably went wrong. Most recent error: %w", lastErr)
}

type liveWriter struct {
}

func (l *liveWriter) Write(p []byte) (n int, err error) {
	return os.Stdout.Write(p)
}

type progressReader struct {
	reader   io.Reader
	received int64 // Total # of bytes transferred
	length   int64 // Expected length
	progress float64
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.reader.Read(b)
	if n > 0 {
		p.received += int64(n)
		p.progress = float64(p.received) / float64(p.length)
	}
	return n, err
}

func runCommand(bin string, args ...string) (string, string, *ExecError) {
	exe := ""
	if path, err := exec.LookPath(bin); err == nil {
		exe = path
	}

	if _, err := os.Stat(exe); os.IsNotExist(err) || exe == "" {
		return "", "", &ExecError{stderr: "", stdout: "", err: "Could not locate " + bin + " on this machine. Is it installed?", command: bin + " " + strings.Join(args, " ")}
	}

	cmd := exec.Command(exe, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return "", "", &ExecError{stderr: stderr.String(), stdout: stdout.String(), err: err.Error(), command: bin + " " + strings.Join(args, " ")}
	}
	return stdout.String(), stderr.String(), nil
}

type ExecError struct {
	stderr  string
	stdout  string
	err     string
	command string
}

func (e *ExecError) Error() string {
	return fmt.Sprintf("error(%v): %v. Stderr: %v", e.command, e.err, e.stderr)
}

func getOpenPort() int {
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		panic(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	return port
}

func findFile(root string, filename string) string {
	file := ""
	symlinkFollower := func(path string, info os.FileInfo, err error) error {

		if info.Mode()&os.ModeSymlink == os.ModeSymlink {
			finalPath, err := filepath.EvalSymlinks(path)
			if err != nil {
				return err
			}
			if info.IsDir() {
				f := findFile(finalPath, path)
				if f != "" {
					file = f
				}
				return nil
			}
			path = finalPath
		}

		if filepath.Base(path) == filename {
			file = path
		}

		return nil
	}

	filepath.Walk(root, symlinkFollower)
	return file
}
