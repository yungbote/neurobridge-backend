package prompts

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)


// Spec is the minimal declaration format you want to write
type Spec struct {
	Name				PromptName
	Version			int
	SchemaName	string
	Schema			func() map[string]any
	// The can be plain strings or go templates using {{.Field}} from input
	System			string
	User				string
	Validators  []Validator
}

// MakeTemplate compiles a Spec into a Template (runtime type)
func MakeTemplate(s Spec) (Template, error) {
	if strings.TrimSpace(string(s.Name)) == "" {
		return Template{}, fmt.Errorf("missing prompt name")
	}
	if s.Version <= 0 {
		return Template{}, fmt.Errorf("invalid version for %s", s.Name)
	}
	if strings.TrimSpace(s.SchemaName) == "" {
		return Template{}, fmt.Errorf("missing schema name for %s", s.Name)
	}
	if s.Schema == nil {
		return Template{}, fmt.Errorf("missing schema func for %s", s.Name)
	}
	sysT, err := template.New("system").Option("missingkey=zero").Parse(s.System)
	if err != nil {
		return Template{}, fmt.Errorf("%s system template parse: %w", s.Name, err)
	}
	userT, err := template.New("user").Option("missingkey=zero").Parse(s.User)
	if err != nil {
		return Template{}, fmt.Errorf("%s user template parse: %w", s.Name, err)
	}
	render := func(t *template.Template, in Input) string {
		var b bytes.Buffer
		_ = t.Execute(&b, in)
		return strings.TrimSpace(b.String())
	}
	tt := Template{
		Name:				s.Name,
		Version:		s.Version,
		SchemaName: s.SchemaName,
		Schema:			s.Schema,
		System:			func(in Input) string { return render(sysT, in) },
		User:				func(in Input) string { return render(userT, in) },
	}
	if len(s.Validators) > 0 {
		tt.Validate = func(in Input) error {
			for _, v := range s.Validators {
				if v == nil {
					continue
				}
				if err := v(in); err != nil {
					return err
				}
			}
			return nil
		}
	}
	return tt, nil
}

// RegisterSpec is the one-liner to call in RegisterAll().
func RegisterSpec(s Spec) {
	t, err := MakeTemplate(s)
	if err !=  nil {
		panic(err)
	}
	Register(t)
}










