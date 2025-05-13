package docker

import (
	"fmt"
	"path/filepath"
	"strings"
)

// FolderDockerInfo holds the absolute path to a folder and a Docker-friendly name.
type DockerFolder struct {
	Path      string
	ImageName string
}

// GetFolderDockerInfo parses an input of the form
//
//	"<path>[:<name>]"
//
// and returns the absolute path plus either the base folder name
// or the provided override name.
func GetFolderDockerInfo(input string) (DockerFolder, error) {
	// Split into path and optional override-name
	parts := strings.SplitN(input, ":", 2)
	rawPath := parts[0]
	overrideName := ""
	if len(parts) == 2 {
		overrideName = parts[1]
	}

	// If someone passed ":bob", treat that as "." for the path
	if rawPath == "" {
		rawPath = "."
	}

	// Resolve to absolute path
	absPath, err := filepath.Abs(rawPath)
	if err != nil {
		return DockerFolder{}, fmt.Errorf("could not get absolute path of %q: %w", rawPath, err)
	}

	// Determine the name: override if given, else use the base folder name
	name := overrideName
	if name == "" {
		name = filepath.Base(absPath)
		if name == "" {
			name = absPath
		}
	}

	return DockerFolder{
		Path:      absPath,
		ImageName: name,
	}, nil
}
