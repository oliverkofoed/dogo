package constructor

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"os"

	"github.com/oliverkofoed/dogo/neaterror"
	"github.com/oliverkofoed/dogo/schema"
)

type TemplateCreator func(location string, template string, templateVars map[string]interface{}) (schema.Template, error)

type Constructor struct {
	typ             reflect.Type
	fields          []*constructorField
	canValidate     bool
	templateCreator TemplateCreator
}

func (c *Constructor) errf(v []map[string]interface{}, format string, args ...interface{}) error {
	m := make(map[string]interface{})
	for _, x := range v {
		for k, v := range x {
			m[k] = v
		}
	}
	return neaterror.New(m, format, args...)
}

type Validate interface {
	Validate() error
}

type constructorField struct {
	field           reflect.StructField
	fieldIndex      int
	lowname         string
	description     string
	required        bool
	typestring      string
	isString        bool
	isInt           bool
	isBool          bool
	isStringArray   bool
	isTemplateArray bool
	isTemplate      bool
	defaultValue    string
	defaultEnvValue string
}

func New(prototype interface{}, templateCreator TemplateCreator) *Constructor {
	typ := reflect.TypeOf(prototype)

	// only work on pointers to structs.
	if typ.Kind() != reflect.Ptr {
		panic(fmt.Sprintf("value should be a pointer to a struct. was: %v", typ.Kind()))
	}
	typ = typ.Elem()
	if typ.Kind() != reflect.Struct {
		panic(fmt.Sprintf("%v type can't have attributes inspected", typ.Kind()))
	}

	c := &Constructor{
		typ:             typ,
		templateCreator: templateCreator,
	}

	// does the type have a validate method?
	if _, ok := prototype.(Validate); ok {
		c.canValidate = true
	}

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !field.Anonymous {
			// find the lowercase field name.
			lowname := strings.ToLower(field.Name)

			// add the field
			typestring := field.Type.String()

			c.fields = append(c.fields, &constructorField{
				field:           field,
				fieldIndex:      i,
				lowname:         lowname,
				typestring:      typestring,
				isString:        typestring == "string",
				isTemplate:      typestring == "schema.Template",
				isInt:           typestring == "int",
				isBool:          typestring == "bool",
				isStringArray:   typestring == "[]string",
				isTemplateArray: typestring == "[]schema.Template",
				required:        field.Tag.Get("required") == "true",
				description:     field.Tag.Get("description"),
				defaultValue:    field.Tag.Get("default"),
				defaultEnvValue: field.Tag.Get("default_env"),
			})
		}
	}

	return c
}

func (c *Constructor) Construct(path string, values []map[string]interface{}, templateVars map[string]interface{}) (interface{}, []error) {
	var errors []error
	instance := reflect.New(c.typ)
	instanceElem := instance.Elem()

	for _, field := range c.fields {
		// find the value for field (if any)
		var fieldValue interface{}
		for _, m := range values {
			if v, found := m[field.lowname]; found {
				fieldValue = v
			}
			if v, found := m[field.field.Name]; found {
				fieldValue = v
			}
			if fieldValue == nil {
				for k, v := range m {
					if strings.ToLower(k) == field.lowname {
						fieldValue = v
					}
				}
			}
		}

		if fieldValue == nil {
			if field.defaultValue != "" || field.defaultEnvValue != "" {
				v := field.defaultValue
				if v == "" {
					v = os.Getenv(field.defaultEnvValue)
				}

				if field.isString || field.isTemplate {
					fieldValue = v
				} else if field.isBool {
					fieldValue = v == "true"
				} else if field.isInt {
					i, err := strconv.Atoi(v)
					if err != nil {
						panic(fmt.Sprintf("bad default value in field tag for %v", field.lowname))
					}
					fieldValue = i
				} else {
					panic("can't set default value for this field")
				}
			} else if field.isTemplate && !field.required {
				fieldValue = ""
			} else if field.isTemplateArray {
				fieldValue = []interface{}{}
			}
		}

		// if we have a field value of the right type, assign it!
		if fieldValue != nil {
			if field.isString {
				if _, ok := fieldValue.(string); !ok {
					errors = append(errors, c.errf(values, "Property '%v' must be of type string. Got: %v (%T)", path+field.lowname, fieldValue, fieldValue))
					continue
				}
			} else if field.isTemplate {
				str, ok := fieldValue.(string)
				if !ok {
					errors = append(errors, c.errf(values, "Property '%v' must be of type string. Got: %v (%T)", path+field.lowname, fieldValue, fieldValue))
					continue
				}

				template, err := c.templateCreator(path+field.lowname, str, templateVars)
				if err != nil {
					errors = append(errors, c.errf(values, "Property '%v' was not a valid template: %v", path+field.lowname, err.Error()))
					continue
				}
				fieldValue = template
			} else if field.isTemplateArray {
				if iarr, ok := fieldValue.([]interface{}); ok {
					newArr := make([]schema.Template, len(iarr), len(iarr))
					for i, v := range iarr {
						if str, ok := v.(string); ok {
							template, err := c.templateCreator(path+field.lowname, str, templateVars)
							if err != nil {
								errors = append(errors, c.errf(values, "Property '%v[%v]' was not a valid template: %v", path+field.lowname, i, err.Error()))
								continue
							}
							newArr[i] = template
						} else {
							errors = append(errors, c.errf(values, "Property '%v' must be of type []string. Got non string value for index %v: %v (%T)", path+field.lowname, i, fieldValue, fieldValue))
							newArr = nil
						}
					}
					fieldValue = newArr
				}
			} else if field.isInt {
				if _, ok := fieldValue.(int); !ok {
					errors = append(errors, c.errf(values, "Property '%v' must be of type int. Got: %v (%T)", path+field.lowname, fieldValue, fieldValue))
					continue
				}
			} else if field.isBool {
				if _, ok := fieldValue.(bool); !ok {
					errors = append(errors, c.errf(values, "Property '%v' must be of type bool. Got: %v (%T)", path+field.lowname, fieldValue, fieldValue))
					continue
				}
			} else if field.isStringArray {
				if iarr, ok := fieldValue.([]interface{}); ok {
					newArr := make([]string, len(iarr), len(iarr))
					for i, v := range iarr {
						if str, ok := v.(string); ok {
							newArr[i] = str
						} else {
							errors = append(errors, c.errf(values, "Property '%v' must be of type []string. Got non string value for index %v: %v (%T)", path+field.lowname, i, fieldValue, fieldValue))
							newArr = nil
						}
					}
					if newArr == nil {
						continue
					}
					fieldValue = newArr
				}
				if _, ok := fieldValue.([]string); !ok {
					errors = append(errors, c.errf(values, "Property '%v' must be of type []string. Got: %v (%T)", path+field.lowname, fieldValue, fieldValue))
					continue
				}
			} else {
				panic("Unknown field type!")
			}

			x := instanceElem.Field(field.fieldIndex)
			x.Set(reflect.ValueOf(fieldValue))
		} else if field.required {
			if field.description != "" {
				errors = append(errors, c.errf(values, "Missing required field '%v' of type %v: %v", path+field.lowname, field.typestring, field.description))
			} else {
				errors = append(errors, c.errf(values, "Missing required field '%v' of type %v", path+field.lowname, field.typestring))
			}
		}
	}

	ival := instance.Interface()
	if v, ok := ival.(Validate); ok {
		err := v.Validate()
		if err != nil {
			errors = append(errors, c.errf(values, err.Error()))
		}
	}

	return instance.Interface(), errors
}
