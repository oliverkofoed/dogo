package main

import (
	"bytes"
	"fmt"

	"github.com/oliverkofoed/dogo/neaterror"
	"github.com/oliverkofoed/dogo/term"
)

func printErrors(errs []error) {
	if errs != nil {
		first := true
		for _, err := range errs {
			if !first {
				fmt.Println()
			}
			fmt.Println(neaterror.String("Config error: ", err, term.IsTerminal))
			first = false
		}
		return
	}
}

var spaces = []rune("                                                                            ")
var dashes = []rune("----------------------------------------------------------------------------")

func runChar(input []rune, length int) string {
	if length < 0 {
		return ""
	}
	if length < len(input) {
		return string(input[:length])
	}

	b := bytes.NewBuffer(nil)
	for i := 0; i != length; i++ {
		b.WriteString(string(input[0]))
	}
	return b.String()
}

/*

func getFile(t schema.Template, extraArgs map[string]interface{}) (io.ReadCloser, int64, os.FileMode, error) {
	file, err := t.Render(extraArgs)
	if err != nil {
		return nil, 0, 0, err
	}

	// default permission
	m, _ := strconv.ParseInt("0740", 8, 32)
	defaultMode := os.FileMode(m)

	if strings.HasPrefix(file, "inline:") {
		rest := []byte(file[len("inline:"):])
		return &byteReadCloser{r: bytes.NewReader([]byte(rest))}, int64(len(rest)), defaultMode, nil
	} else if strings.HasPrefix(file, "vault:") {
		parts := strings.Split(file, ":")
		if len(parts) != 3 {
			return nil, 0, 0, fmt.Errorf("Files identified by 'vault:' must have be of the form 'vault:file.vault:vaultkey'. got: %v", file)
		}
		content, err := getOpenVaultFile(parts[1], parts[2])
		if err != nil {
			return nil, 0, 0, err
		}
		return &byteReadCloser{r: bytes.NewReader(content)}, int64(len(content)), defaultMode, nil
	} else if strings.HasPrefix(file, "file:") {
		return getFileLocal(file[len("file:"):])
	}
	return getFileLocal(file)
}

func getFileLocal(path string) (io.ReadCloser, int64, os.FileMode, error) {
	s, err := os.Stat(path)
	if err != nil {
		return nil, 0, 0, err
	}
	f, err := os.Open(path)
	return f, s.Size(), s.Mode(), err
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
*/
