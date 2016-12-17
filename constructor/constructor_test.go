package constructor

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/oliverkofoed/dogo/neaterror"
	"github.com/oliverkofoed/dogo/schema"
	"github.com/oliverkofoed/jet"
)

type resourceExtras struct {
	Count    int
	Packages []string
	Templ    schema.Template
	TemplArr []schema.Template
}

type User struct {
	Name string
}

func TestConstructor(t *testing.T) {
	set := jet.NewSet(func(w io.Writer, b []byte) { w.Write(b) })
	set.AddGlobal("_", "Hi There")

	var resourceExtrasConstructor = New(&resourceExtras{}, func(location string, templateStr string, templateVars map[string]interface{}) (schema.Template, error) {
		templ, err := set.ParseInline(location, templateStr)
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
	})

	vars := map[string]interface{}{
		"hi": "nook",
	}

	x := map[string]interface{}{
		"count":    43,
		"packages": []interface{}{"hello", "world"},
		"templ":    "hi {{greeting}}",
		"templarr": []interface{}{"hey", "there"},
	}
	a, err := resourceExtrasConstructor.Construct("", []map[string]interface{}{
		0: x,
	}, vars)
	if err != nil {
		t.Fail()
		return
	}

	extra, ok := a.(*resourceExtras)
	if !ok {
		t.Fail()
		return
	}

	if extra.Count != 43 {
		t.Fail()
		return
	}

	set.AddGlobal("greeting", "there")
	output, templErr := extra.Templ.Render(nil)
	if templErr != nil {
		t.Error("templ err: ", templErr)
		return
	}

	if output != "hi there" {
		t.Error("not right output:", output)
		return
	}
}

type template struct {
	originalTemplate string
	template         *jet.Template
	templateVars     jet.VarMap
	getOpenVaultFile func(filename string, key string) ([]byte, error)
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
			"!template": t.originalTemplate,
		}, s)
	}
	return buf.String(), nil
}

func (t *template) RenderFile(extraArgs map[string]interface{}) (io.ReadCloser, int64, os.FileMode, error) {
	panic("NOT IMPLEMENTED")
}

func (t *template) RenderFileBytes(extraArgs map[string]interface{}) ([]byte, error) {
	panic("NOT IMPLEMENTED")
}
