package file

import (
	"bytes"
	"crypto/md5"
	"io"
	"os"
	"strconv"

	"fmt"

	"io/ioutil"

	"path/filepath"

	"github.com/oliverkofoed/dogo/commandtree"
	"github.com/oliverkofoed/dogo/schema"
	"github.com/oliverkofoed/dogo/snobgob"
)

type File struct {
	RemotePath schema.Template `required:"true" description:"The remote file path"`
	File       schema.Template `required:"true" description:"The file to put on the target system"`
	Permission schema.Template `default:"" description:"The filemode to set on the file"`
	Checksum   bool            `default:"true" description:"Calculate a checksum to check for file equality"`
}

type state struct {
	Files map[string]*fileInfo
}

type fileInfo struct {
	Size     int64
	Mode     uint32
	Checksum []byte
}

// Manager is the main entry point to this Dogo Module
var Manager = schema.ModuleManager{
	Name:            "file",
	ModulePrototype: &File{},
	StatePrototype:  &state{},
	GobRegister: func() {
		snobgob.Register(&fileInfo{})
		snobgob.Register(&writeFileCommand{})
		snobgob.Register(make(map[string]bool))
	},
	CalculateGetStateQuery: func(c *schema.CalculateGetStateQueryArgs) (interface{}, error) {
		modules := c.Modules.([]*File)
		query := make(map[string]bool)
		for _, f := range modules {
			path, err := f.RemotePath.Render(nil)
			if err != nil {
				return nil, err
			}

			query[path] = f.Checksum
		}
		return query, nil
	},
	GetState: func(query interface{}) (interface{}, error) {
		state := &state{Files: make(map[string]*fileInfo)}

		for path, useChecksum := range query.(map[string]bool) {
			// Get state for each of the files requested.
			if stat, err := os.Stat(path); err == nil {
				info := &fileInfo{}
				info.Size = stat.Size()
				info.Mode = uint32(stat.Mode())
				if useChecksum {
					file, err := os.Open(path)
					if err != nil {
						return nil, fmt.Errorf("Could not open %v for generating checksum.", path)
					}

					checksum, err := calcChecksum(file)
					if err != nil {
						file.Close()
						return nil, fmt.Errorf("Error generating checksum for %v: %v", path, err)
					}
					file.Close()
					info.Checksum = checksum
				}

				state.Files[path] = info
			}
		}

		return state, nil
	},
	CalculateCommands: func(c *schema.CalculateCommandsArgs) error {
		// this module can't change any state. It's purely for reporting purposes.
		remoteState := c.State.(*state)
		modules := c.Modules.([]*File)

		for _, f := range modules {
			path, err := f.RemotePath.Render(nil)
			if err != nil {
				return err
			}

			// compare with remote (if we have it)
			if remote, found := remoteState.Files[path]; found {
				localFile, localSize, localMode, err := getFile(c, f)
				if err != nil {
					return err
				}

				if localSize == remote.Size && uint32(localMode) == remote.Mode {
					if f.Checksum {
						localChecksum, err := calcChecksum(localFile)
						if err != nil {
							localFile.Close()
							return err
						}
						if bytes.Equal(localChecksum, remote.Checksum) {
							// yay, they're equal, nothing to do!
							localFile.Close()
							continue
						}
					} else {
						// yay, file is close enough!
						localFile.Close()
						continue
					}
				}
				localFile.Close()
			}

			// read file content
			localFile, _, localMode, err := getFile(c, f)
			if err != nil {
				return err
			}

			content, err := ioutil.ReadAll(localFile)
			if err != nil {
				localFile.Close()
				return err
			}
			localFile.Close()

			// add upload command
			c.RemoteCommands.Add("Save "+path, &writeFileCommand{
				Path:     path,
				Content:  content,
				FileMode: uint32(localMode),
			})
		}

		return nil
	},
}

func calcChecksum(r io.Reader) ([]byte, error) {
	hash := md5.New()
	if _, err := io.Copy(hash, r); err != nil {
		return nil, err
	}
	return hash.Sum(nil)[:16], nil
}

func getFile(c *schema.CalculateCommandsArgs, f *File) (io.ReadCloser, int64, os.FileMode, error) {
	// calculate local permission override, if any
	perm, err := f.Permission.Render(nil)
	if err != nil {
		return nil, 0, 0, err
	}

	localFile, localSize, localMode, err := f.File.RenderFile(nil)
	if perm != "" {
		mode, err := parsePermission(perm)
		if err != nil {
			return nil, 0, 0, err
		}
		localMode = mode
	}
	return localFile, localSize, localMode, err
}

func parsePermission(perm string) (os.FileMode, error) {
	i, err := strconv.ParseInt(perm, 8, 32)
	if err != nil {
		return 0, err
	}
	return os.FileMode(i), nil
}

type writeFileCommand struct {
	commandtree.Command
	Path     string
	Content  []byte
	FileMode uint32
}

func (c *writeFileCommand) Execute() {
	// remove it first in case it exists, to ensure perm gets set correctly.
	os.Remove(c.Path)

	// create directories up to the file
	dirmode, _ := parsePermission("0777")
	err := os.MkdirAll(filepath.Dir(c.Path), dirmode)
	if err != nil {
		c.Errf("Could not create directory structure up to file: %v", c.Path)
		return
	}

	err = ioutil.WriteFile(c.Path, c.Content, os.FileMode(c.FileMode))
	if err != nil {
		c.Errf("Could not write %v bytes %v: %v", len(c.Content), c.Path, err.Error())
	}

	if s, err := os.Stat(c.Path); err == nil {
		if s.Mode() != os.FileMode(c.FileMode) {
			c.Errf("The file changed filemode directly after writing. It's now %v (%v) instead of the requested %v (%v). This will cause the file to be reuploaded on every deploy.", s.Mode(), strconv.FormatUint(uint64(s.Mode()), 8), os.FileMode(c.FileMode), strconv.FormatUint(uint64(c.FileMode), 8))
		}
	} else {
		c.Errf("Could not check (stat) file after writing. Err: %v", err)
	}
}
