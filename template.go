package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"reflect"
	"strconv"
	"strings"

	"io/ioutil"

	"github.com/coreos/etcd/version"
	"github.com/oliverkofoed/dogo/neaterror"
	"github.com/oliverkofoed/dogo/schema"
	"github.com/oliverkofoed/jet"
)

type templateSource struct {
	templateSet *jet.Set
}

func (t *templateSource) NewTemplate(location string, templateStr string, templateVars map[string]interface{}) (schema.Template, error) {
	templ, err := t.templateSet.ParseInline(location, templateStr)
	if err != nil {
		return nil, err
	}

	vars := make(jet.VarMap)
	for k, v := range templateVars {
		vars.Set(k, v)
	}

	return &template{
		template:         templ,
		templateVars:     vars,
		originalTemplate: templateStr,
	}, nil
}

func (t *templateSource) AddGlobal(key string, value interface{}) {
	t.templateSet.AddGlobal(key, value)
}

func newTemplateSource() *templateSource {
	templateSet := jet.NewSet(func(w io.Writer, b []byte) { w.Write(b) })
	templateSet.AddGlobal("version", version.Version)
	templateSet.AddGlobalFunc("vaultstring", func(a jet.Arguments) reflect.Value {
		a.RequireNumOfArguments("vaultstring", 2, 2)
		file := a.Get(0).String()
		key := a.Get(1).String()

		v, err := getOpenVault(file)
		if err != nil {
			panic(err)
		}
		str, err := v.GetString(key)
		if err != nil {
			panic(err)
		}

		return reflect.ValueOf(str)
	})
	templateSet.AddGlobalFunc("vaultfile", func(a jet.Arguments) reflect.Value {
		a.RequireNumOfArguments("vaultfile", 2, 2)
		file := a.Get(0).String()
		key := a.Get(1).String()

		v, err := getOpenVault(file)
		if err != nil {
			panic(err)
		}
		bytes, err := v.GetBytes(key)
		if err != nil {
			panic(err)
		}

		return reflect.ValueOf(bytes)
	})
	templateSet.AddGlobalFunc("base64", func(a jet.Arguments) reflect.Value {
		a.RequireNumOfArguments("base64", 1, 1)
		return reflect.ValueOf(base64.StdEncoding.EncodeToString(a.Get(0).Bytes()))
	})
	templateSet.AddGlobalFunc("base64url", func(a jet.Arguments) reflect.Value {
		a.RequireNumOfArguments("base64url", 1, 1)
		return reflect.ValueOf(base64.URLEncoding.EncodeToString(a.Get(0).Bytes()))
	})
	templateSet.AddGlobalFunc("int", func(a jet.Arguments) reflect.Value {
		a.RequireNumOfArguments("int", 1, 1)
		v := a.Get(0).Interface()
		i, ok := v.(int)

		if !ok {
			panic(fmt.Sprintf("The given value %v was not an integer, but an %T", v, v))
		}

		return reflect.ValueOf(i)
	})
	templateSet.AddGlobalFunc("lookup", func(a jet.Arguments) reflect.Value {
		//THIS METHOD IS A TEMPORARY WORKAROUND METHOD WHILE WE WAIT FOR https://github.com/CloudyKit/jet/issues/41
		a.RequireNumOfArguments("int", 2, 2)

		i := a.Get(1)
		index := int64(0)
		if i.Kind() == reflect.Int64 {
			index = i.Int()
		}

		v := a.Get(0)
		if v.Kind() == reflect.Slice {
			return v.Index(int(index))
		}

		panic(fmt.Sprintf("bad!"))
	})

	return &templateSource{
		templateSet: templateSet,
	}
}

type template struct {
	originalTemplate string
	template         *jet.Template
	templateVars     jet.VarMap
	//getOpenVaultFile func(filename string, key string) ([]byte, error)
}

func (t *template) Render(override map[string]interface{}) (string, error) {
	variables := t.templateVars
	if override != nil {
		variables = make(jet.VarMap)
		for k, v := range t.templateVars {
			variables.Set(k, v)
		}
		for k, v := range override {
			variables.Set(k, v)
		}
	}

	buf := bytes.NewBuffer(nil)
	err := t.template.Execute(buf, variables, nil)
	if err != nil {
		s := err.Error()
		i := strings.Index(s, "): ")
		if i > 0 {
			s = /*"template error: " +*/ s[i+3:]
		}
		return "", neaterror.New(map[string]interface{}{
			"!template":  t.originalTemplate,
			"localscope": scopeMap(variables),
		}, s)
	}
	return buf.String(), nil
}

func (t *template) RenderFile(extraArgs map[string]interface{}) (io.ReadCloser, int64, os.FileMode, error) {
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

func (t *template) RenderFileBytes(extraArgs map[string]interface{}) ([]byte, error) {
	r, _, _, err := t.RenderFile(extraArgs)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	b, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, err
	}

	return b, nil
}

func scopeMap(m jet.VarMap) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range m {
		val := v.Interface()
		if sm, ok := val.(jet.VarMap); ok {
			val = scopeMap(sm)
		}
		result[k] = val
	}

	return result
}

func getFileLocal(path string) (io.ReadCloser, int64, os.FileMode, error) {
	s, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, 0, fmt.Errorf("No such file: '%v'", path)
		}
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
