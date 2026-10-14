package main

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Spec is the subset of an OpenAPI 3.0 document the generator understands.
type Spec struct {
	Info struct {
		Title       string `yaml:"title"`
		Version     string `yaml:"version"`
		Description string `yaml:"description"`
	} `yaml:"info"`
	Servers []struct {
		URL         string `yaml:"url"`
		Description string `yaml:"description"`
	} `yaml:"servers"`
	Paths      PathItems `yaml:"paths"`
	Components struct {
		Schemas       NamedSchemas            `yaml:"schemas"`
		Parameters    map[string]*Parameter   `yaml:"parameters"`
		Responses     map[string]*Response    `yaml:"responses"`
		RequestBodies map[string]*RequestBody `yaml:"requestBodies"`
	} `yaml:"components"`
}

// Schema is an OpenAPI schema object.
type Schema struct {
	Ref                  string           `yaml:"$ref"`
	Type                 string           `yaml:"type"`
	Format               string           `yaml:"format"`
	Title                string           `yaml:"title"`
	Description          string           `yaml:"description"`
	Enum                 []any            `yaml:"enum"`
	Properties           Properties       `yaml:"properties"`
	Required             []string         `yaml:"required"`
	Items                *Schema          `yaml:"items"`
	AllOf                []*Schema        `yaml:"allOf"`
	OneOf                []*Schema        `yaml:"oneOf"`
	AnyOf                []*Schema        `yaml:"anyOf"`
	AdditionalProperties *AdditionalProps `yaml:"additionalProperties"`
	Nullable             bool             `yaml:"nullable"`
	Deprecated           bool             `yaml:"deprecated"`
	Discriminator        *Discriminator   `yaml:"discriminator"`
}

// Discriminator is an OpenAPI discriminator object.
type Discriminator struct {
	PropertyName string            `yaml:"propertyName"`
	Mapping      map[string]string `yaml:"mapping"`
}

// Property is a named schema inside "properties", kept in document order.
type Property struct {
	Name   string
	Schema *Schema
}

// Properties preserves the declaration order of object properties.
type Properties []Property

func (p *Properties) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: properties must be a mapping", n.Line)
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		s := &Schema{}
		if err := n.Content[i+1].Decode(s); err != nil {
			return err
		}
		*p = append(*p, Property{Name: n.Content[i].Value, Schema: s})
	}
	return nil
}

// NamedSchemas is components.schemas in document order.
type NamedSchemas struct {
	Names  []string
	ByName map[string]*Schema
}

func (ns *NamedSchemas) UnmarshalYAML(n *yaml.Node) error {
	var props Properties
	if err := props.UnmarshalYAML(n); err != nil {
		return err
	}
	ns.ByName = map[string]*Schema{}
	for _, p := range props {
		ns.Names = append(ns.Names, p.Name)
		ns.ByName[p.Name] = p.Schema
	}
	return nil
}

// AdditionalProps models additionalProperties, which is either a bool or a schema.
type AdditionalProps struct {
	Allowed bool
	Schema  *Schema
}

func (a *AdditionalProps) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		return n.Decode(&a.Allowed)
	}
	a.Allowed = true
	a.Schema = &Schema{}
	return n.Decode(a.Schema)
}

// Parameter is an OpenAPI parameter object.
type Parameter struct {
	Ref         string  `yaml:"$ref"`
	Name        string  `yaml:"name"`
	In          string  `yaml:"in"`
	Description string  `yaml:"description"`
	Required    bool    `yaml:"required"`
	Deprecated  bool    `yaml:"deprecated"`
	Style       string  `yaml:"style"`
	Explode     *bool   `yaml:"explode"`
	Schema      *Schema `yaml:"schema"`
}

// MediaType is an entry of a content map.
type MediaType struct {
	Schema *Schema `yaml:"schema"`
}

// RequestBody is an OpenAPI request body object.
type RequestBody struct {
	Ref         string                `yaml:"$ref"`
	Description string                `yaml:"description"`
	Required    bool                  `yaml:"required"`
	Content     map[string]*MediaType `yaml:"content"`
}

// Response is an OpenAPI response object.
type Response struct {
	Ref         string                `yaml:"$ref"`
	Description string                `yaml:"description"`
	Content     map[string]*MediaType `yaml:"content"`
}

// Operation is an OpenAPI operation object.
type Operation struct {
	OperationID   string       `yaml:"operationId"`
	Summary       string       `yaml:"summary"`
	Description   string       `yaml:"description"`
	Tags          []string     `yaml:"tags"`
	Deprecated    bool         `yaml:"deprecated"`
	Internal      bool         `yaml:"x-internal"`
	RateLimit     string       `yaml:"x-rate-limit-category"`
	Parameters    []*Parameter `yaml:"parameters"`
	RequestBody   *RequestBody `yaml:"requestBody"`
	Responses     Responses    `yaml:"responses"`
	Method        string       `yaml:"-"`
	Path          string       `yaml:"-"`
	PathItemParam []*Parameter `yaml:"-"`
}

// Responses keeps status codes in document order.
type Responses struct {
	Codes  []string
	ByCode map[string]*Response
}

func (r *Responses) UnmarshalYAML(n *yaml.Node) error {
	r.ByCode = map[string]*Response{}
	for i := 0; i+1 < len(n.Content); i += 2 {
		resp := &Response{}
		if err := n.Content[i+1].Decode(resp); err != nil {
			return err
		}
		r.Codes = append(r.Codes, n.Content[i].Value)
		r.ByCode[n.Content[i].Value] = resp
	}
	return nil
}

// PathItems holds every operation in document order.
type PathItems struct {
	Operations []*Operation
}

var httpMethods = []string{"get", "put", "post", "delete", "patch", "head", "options"}

func (p *PathItems) UnmarshalYAML(n *yaml.Node) error {
	for i := 0; i+1 < len(n.Content); i += 2 {
		path := n.Content[i].Value
		item := n.Content[i+1]
		var shared []*Parameter
		ops := map[string]*Operation{}
		for j := 0; j+1 < len(item.Content); j += 2 {
			key := item.Content[j].Value
			val := item.Content[j+1]
			if key == "parameters" {
				if err := val.Decode(&shared); err != nil {
					return err
				}
				continue
			}
			for _, m := range httpMethods {
				if key == m {
					op := &Operation{}
					if err := val.Decode(op); err != nil {
						return fmt.Errorf("%s %s: %w", m, path, err)
					}
					op.Method = strings.ToUpper(m)
					op.Path = path
					ops[m] = op
				}
			}
		}
		for _, m := range httpMethods {
			if op, ok := ops[m]; ok {
				op.PathItemParam = shared
				p.Operations = append(p.Operations, op)
			}
		}
	}
	return nil
}

func loadSpec(path string) (*Spec, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	s := &Spec{}
	if err := yaml.Unmarshal(b, s); err != nil {
		return nil, err
	}
	return s, nil
}
