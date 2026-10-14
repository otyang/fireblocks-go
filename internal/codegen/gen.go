package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Kind classifies a generated Go type; it drives pointer and omitempty decisions.
type Kind int

const (
	KAny Kind = iota
	KScalar
	KEnum
	KStruct
	KUnion
	KSlice
	KMap
	KFile
)

// TRef is a resolved Go type expression.
type TRef struct {
	Expr string
	Kind Kind
}

// Decl is one top-level Go declaration destined for File.
type Decl struct {
	Name string
	File string
	Code string
}

// Gen turns a Spec into Go source.
type Gen struct {
	spec     *Spec
	idents   identSet
	comp     map[string]string // component schema name -> Go type name
	compKind map[string]Kind
	kindBusy map[string]bool
	decls    []*Decl
	file     string
}

func newGen(spec *Spec) *Gen {
	return &Gen{
		spec:     spec,
		idents:   identSet{},
		comp:     map[string]string{},
		compKind: map[string]Kind{},
		kindBusy: map[string]bool{},
	}
}

const schemaPrefix = "#/components/schemas/"

func refName(ref string) string { return strings.TrimPrefix(ref, schemaPrefix) }

func (g *Gen) component(name string) *Schema { return g.spec.Components.Schemas.ByName[name] }

// resolve follows $ref chains to a concrete schema.
func (g *Gen) resolve(s *Schema) *Schema {
	for i := 0; s != nil && s.Ref != "" && i < 32; i++ {
		s = g.component(refName(s.Ref))
	}
	if s == nil {
		return &Schema{}
	}
	return s
}

func isNull(s *Schema) bool {
	return s.Type == "null" || (s.Ref == "" && len(s.Enum) == 1 && s.Enum[0] == nil)
}

// isConstraintOnly reports whether s only adds constraints (e.g. "required") without shape.
func isConstraintOnly(s *Schema) bool {
	return s.Ref == "" && s.Type == "" && len(s.Properties) == 0 && s.Items == nil &&
		len(s.AllOf)+len(s.OneOf)+len(s.AnyOf) == 0 && len(s.Enum) == 0 && s.AdditionalProperties == nil
}

// normalize rewrites compositions into one of: a $ref, a union (OneOf), or a plain schema.
func (g *Gen) normalize(s *Schema) *Schema {
	return g.normalizeDepth(s, 0)
}

func (g *Gen) normalizeDepth(s *Schema, depth int) *Schema {
	c := *s
	if depth > 16 {
		return &c
	}
	if len(c.Enum) > 0 {
		var vals []any
		for _, v := range c.Enum {
			if v == nil {
				c.Nullable = true
				continue
			}
			vals = append(vals, v)
		}
		c.Enum = vals
	}

	variants := append(append([]*Schema{}, c.OneOf...), c.AnyOf...)
	if len(variants) > 0 && len(c.AllOf) == 0 {
		var keep []*Schema
		for _, v := range variants {
			if isNull(v) {
				c.Nullable = true
				continue
			}
			keep = append(keep, v)
		}
		c.OneOf, c.AnyOf = nil, nil
		if len(c.Properties) > 0 {
			// Object with properties plus alternatives: flatten everything into one struct.
			self := c
			union := &Schema{OneOf: keep}
			return g.normalizeDepth(&Schema{
				AllOf:       []*Schema{&self, union},
				Description: c.Description,
				Nullable:    c.Nullable,
				Deprecated:  c.Deprecated,
			}, depth+1)
		}
		switch len(keep) {
		case 0:
			return &Schema{Description: c.Description, Nullable: true}
		case 1:
			out := *g.normalizeDepth(keep[0], depth+1)
			if out.Description == "" {
				out.Description = c.Description
			}
			out.Nullable = out.Nullable || c.Nullable
			out.Deprecated = out.Deprecated || c.Deprecated
			return &out
		}
		c.OneOf = keep
		return &c
	}

	if len(c.AllOf) > 0 {
		var substantive []*Schema
		for _, p := range c.AllOf {
			if !isConstraintOnly(p) {
				substantive = append(substantive, p)
			}
		}
		if len(c.Properties) == 0 && len(substantive) == 1 {
			out := *g.normalizeDepth(substantive[0], depth+1)
			if out.Ref != "" {
				out = Schema{Ref: out.Ref}
			}
			if c.Description != "" {
				out.Description = c.Description
			}
			out.Nullable = out.Nullable || c.Nullable
			out.Deprecated = out.Deprecated || c.Deprecated
			if out.Ref == "" {
				// Copy before appending: out shares its slices with the spec.
				out.Required = append(append([]string(nil), out.Required...), c.Required...)
				for _, p := range c.AllOf {
					out.Required = append(out.Required, p.Required...)
				}
			}
			return &out
		}
		merged := &Schema{Type: "object", Description: c.Description, Nullable: c.Nullable, Deprecated: c.Deprecated}
		self := c
		self.AllOf = nil
		g.mergeInto(merged, &self, true, depth+1)
		for _, p := range c.AllOf {
			g.mergeInto(merged, p, true, depth+1)
		}
		return merged
	}
	return &c
}

// mergeInto copies the properties of src (after resolution) into dst.
func (g *Gen) mergeInto(dst, src *Schema, keepRequired bool, depth int) {
	if depth > 16 {
		return
	}
	src = g.resolve(src)
	n := g.normalizeDepth(src, depth+1)
	if n.Ref != "" {
		g.mergeInto(dst, n, keepRequired, depth+1)
		return
	}
	if len(n.OneOf) > 0 {
		for _, v := range n.OneOf {
			g.mergeInto(dst, v, false, depth+1)
		}
	}
	for _, p := range n.Properties {
		dup := false
		for _, q := range dst.Properties {
			if q.Name == p.Name {
				dup = true
				break
			}
		}
		if !dup {
			dst.Properties = append(dst.Properties, p)
		}
	}
	if keepRequired {
		dst.Required = append(dst.Required, n.Required...)
	}
	if dst.AdditionalProperties == nil {
		dst.AdditionalProperties = n.AdditionalProperties
	}
}

func effType(n *Schema) string {
	switch {
	case n.Type != "":
		return n.Type
	case len(n.Properties) > 0 || n.AdditionalProperties != nil:
		return "object"
	case n.Items != nil:
		return "array"
	case len(n.Enum) > 0:
		switch n.Enum[0].(type) {
		case string:
			return "string"
		case int, int64:
			return "integer"
		case float64:
			return "number"
		case bool:
			return "boolean"
		}
	}
	return ""
}

func isStringEnum(n *Schema) bool {
	return len(n.Enum) > 0 && effType(n) == "string"
}

func (g *Gen) compKindOf(name string) Kind {
	if k, ok := g.compKind[name]; ok {
		return k
	}
	s := g.component(name)
	if s == nil {
		return KAny
	}
	if g.kindBusy[name] {
		return KStruct
	}
	g.kindBusy[name] = true
	k := g.kindOf(s)
	g.kindBusy[name] = false
	g.compKind[name] = k
	return k
}

func (g *Gen) kindOf(s *Schema) Kind {
	if s == nil {
		return KAny
	}
	if s.Ref != "" {
		return g.compKindOf(refName(s.Ref))
	}
	return g.kindOfNormalized(g.normalize(s))
}

func (g *Gen) kindOfNormalized(n *Schema) Kind {
	if n.Ref != "" {
		return g.compKindOf(refName(n.Ref))
	}
	if len(n.OneOf) > 0 {
		return KUnion
	}
	if isStringEnum(n) {
		return KEnum
	}
	switch effType(n) {
	case "object":
		if len(n.Properties) > 0 {
			return KStruct
		}
		return KMap
	case "array":
		return KSlice
	case "string":
		if n.Format == "binary" {
			return KFile
		}
		return KScalar
	case "integer", "number", "boolean":
		return KScalar
	}
	return KAny
}

func scalarType(n *Schema) string {
	switch effType(n) {
	case "integer":
		if n.Format == "int32" {
			return "int32"
		}
		return "int64"
	case "number":
		return "float64"
	case "boolean":
		return "bool"
	}
	return "string"
}

func (g *Gen) refType(ref string) TRef {
	name := refName(ref)
	goName, ok := g.comp[name]
	if !ok {
		return TRef{"any", KAny}
	}
	return TRef{goName, g.compKindOf(name)}
}

func (g *Gen) newType(hint string) string {
	if hint == "" {
		hint = "Anonymous"
	}
	return g.idents.unique(hint)
}

// typeOf returns the Go type for s, generating named declarations for inline
// structs, enums and unions using hint as the base name.
func (g *Gen) typeOf(s *Schema, hint string) TRef {
	if s == nil {
		return TRef{"any", KAny}
	}
	if s.Ref != "" {
		return g.refType(s.Ref)
	}
	n := g.normalize(s)
	if n.Ref != "" {
		return g.refType(n.Ref)
	}
	switch g.kindOfNormalized(n) {
	case KUnion:
		name := g.newType(hint)
		g.genUnion(name, n, "")
		return TRef{name, KUnion}
	case KEnum:
		name := g.newType(hint)
		g.genEnum(name, n, "")
		return TRef{name, KEnum}
	case KStruct:
		name := g.newType(hint)
		g.genStruct(name, n, "")
		return TRef{name, KStruct}
	case KMap:
		if ap := n.AdditionalProperties; ap != nil && ap.Schema != nil {
			v := g.typeOf(ap.Schema, hint+"Value")
			return TRef{"map[string]" + v.Expr, KMap}
		}
		return TRef{"map[string]any", KMap}
	case KSlice:
		it := g.typeOf(n.Items, hint+"Item")
		return TRef{"[]" + it.Expr, KSlice}
	case KFile:
		return TRef{"*File", KFile}
	case KScalar:
		return TRef{scalarType(n), KScalar}
	}
	return TRef{"any", KAny}
}

func (g *Gen) addDecl(name string) *Decl {
	d := &Decl{Name: name, File: g.file}
	g.decls = append(g.decls, d)
	return d
}

// writeDoc writes a Go doc comment. lead is the first sentence.
func writeDoc(b *strings.Builder, indent, lead, desc string, deprecated bool) {
	wrote := false
	if lead != "" {
		fmt.Fprintf(b, "%s// %s\n", indent, lead)
		wrote = true
	}
	desc = cleanText(desc)
	if desc != "" {
		if wrote {
			fmt.Fprintf(b, "%s//\n", indent)
		}
		for _, line := range strings.Split(desc, "\n") {
			line = strings.TrimRight(line, " \t")
			if line == "" {
				fmt.Fprintf(b, "%s//\n", indent)
			} else {
				fmt.Fprintf(b, "%s// %s\n", indent, line)
			}
		}
		wrote = true
	}
	if deprecated {
		if wrote {
			fmt.Fprintf(b, "%s//\n", indent)
		}
		fmt.Fprintf(b, "%s// Deprecated: marked as deprecated in the Fireblocks API specification.\n", indent)
	}
}

func cleanText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.TrimSpace(s)
}

func (g *Gen) genComponents() {
	names := append([]string{}, g.spec.Components.Schemas.Names...)
	sort.Strings(names)
	for _, name := range names {
		goName := g.comp[name]
		g.file = modelFile(goName)
		s := g.component(name)
		lead := fmt.Sprintf("%s defines model for %s.", goName, name)
		var n *Schema
		if s.Ref != "" {
			n = s
		} else {
			n = g.normalize(s)
		}
		if n.Ref != "" {
			t := g.refType(n.Ref)
			d := g.addDecl(goName)
			var b strings.Builder
			writeDoc(&b, "", lead, s.Description, s.Deprecated)
			fmt.Fprintf(&b, "type %s = %s\n", goName, t.Expr)
			d.Code = b.String()
			continue
		}
		desc := n.Description
		switch g.kindOfNormalized(n) {
		case KUnion:
			g.genUnion(goName, n, lead)
		case KEnum:
			g.genEnum(goName, n, lead)
		case KStruct:
			g.genStruct(goName, n, lead)
		default:
			d := g.addDecl(goName)
			var b strings.Builder
			writeDoc(&b, "", lead, desc, n.Deprecated)
			k := g.kindOfNormalized(n)
			t := g.typeOf(n, goName)
			if k == KMap || k == KSlice {
				fmt.Fprintf(&b, "type %s %s\n", goName, t.Expr)
			} else {
				fmt.Fprintf(&b, "type %s = %s\n", goName, t.Expr)
			}
			d.Code = b.String()
		}
	}
}

func modelFile(goName string) string {
	c := strings.ToLower(goName[:1])
	if c[0] < 'a' || c[0] > 'z' {
		c = "other"
	}
	return "models_" + c + ".gen.go"
}

// fieldExpr decides pointer-ness and omitempty for a struct field.
func fieldExpr(t TRef, required, nullable bool) (string, bool) {
	switch t.Kind {
	case KStruct, KUnion:
		return "*" + t.Expr, !required
	case KScalar, KEnum:
		if required && !nullable {
			return t.Expr, false
		}
		return "*" + t.Expr, !required
	}
	return t.Expr, !required
}

func (g *Gen) genStruct(name string, n *Schema, lead string) {
	d := g.addDecl(name)
	if lead == "" {
		lead = name + " is an inline object type."
	}
	req := map[string]bool{}
	for _, r := range n.Required {
		req[r] = true
	}
	used := identSet{}
	var fields strings.Builder
	for i, p := range n.Properties {
		fname := pascal(p.Name)
		if fname == "" {
			fname = "Field" + itoa(i)
		}
		fname = used.unique(fname)
		t := g.typeOf(p.Schema, name+fname)
		ps := g.normalize(p.Schema)
		expr, omit := fieldExpr(t, req[p.Name], p.Schema.Nullable || ps.Nullable)
		desc := p.Schema.Description
		if desc == "" && p.Schema.Ref == "" {
			desc = ps.Description
		}
		writeDoc(&fields, "\t", "", desc, p.Schema.Deprecated)
		tag := p.Name
		if omit {
			tag += ",omitempty"
		}
		fmt.Fprintf(&fields, "\t%s %s `json:%s`\n", fname, expr, strconv.Quote(tag))
	}
	var b strings.Builder
	writeDoc(&b, "", lead, n.Description, n.Deprecated)
	fmt.Fprintf(&b, "type %s struct {\n%s}\n", name, fields.String())
	d.Code = b.String()
}

func (g *Gen) genEnum(name string, n *Schema, lead string) {
	d := g.addDecl(name)
	if lead == "" {
		lead = name + " is an enumeration of string values."
	}
	var b strings.Builder
	writeDoc(&b, "", lead, n.Description, n.Deprecated)
	fmt.Fprintf(&b, "type %s string\n\n", name)
	fmt.Fprintf(&b, "// Known values of %s.\nconst (\n", name)
	seen := map[string]bool{}
	for _, v := range n.Enum {
		val := fmt.Sprint(v)
		if seen[val] {
			continue
		}
		seen[val] = true
		suffix := pascal(val)
		if suffix == "" {
			suffix = "Empty"
		}
		cname := g.idents.unique(name + suffix)
		fmt.Fprintf(&b, "\t%s %s = %s\n", cname, name, strconv.Quote(val))
	}
	b.WriteString(")\n")
	d.Code = b.String()
}

type variant struct {
	label string
	t     TRef
	ref   string
}

func (g *Gen) genUnion(name string, n *Schema, lead string) {
	d := g.addDecl(name)
	if lead == "" {
		lead = name + " is an inline union type."
	}
	labels := identSet{}
	var vs []variant
	for i, v := range n.OneOf {
		var vr variant
		nv := v
		if v.Ref == "" {
			nv = g.normalize(v)
		}
		if nv.Ref != "" {
			vr.t = g.refType(nv.Ref)
			vr.ref = nv.Ref
			vr.label = vr.t.Expr
		} else {
			switch g.kindOfNormalized(nv) {
			case KScalar:
				vr.t = g.typeOf(v, "")
				vr.label = pascal(vr.t.Expr)
			case KMap:
				vr.t = g.typeOf(v, name+"Object")
				vr.label = "Object"
			case KSlice:
				vr.t = g.typeOf(v, name+"Item")
				vr.label = "Array"
			default:
				label := pascal(nv.Title)
				if label == "" {
					label = "Variant" + itoa(i+1)
				}
				vr.t = g.typeOf(v, name+label)
				vr.label = label
			}
		}
		vr.label = labels.unique(vr.label)
		vs = append(vs, vr)
	}

	ctors := make([]string, len(vs))
	for i, v := range vs {
		ctors[i] = g.idents.unique("New" + name + "From" + v.label)
	}

	var b strings.Builder
	writeDoc(&b, "", lead, n.Description, n.Deprecated)
	b.WriteString("//\n// It holds exactly one of the following variants:\n")
	for i, v := range vs {
		fmt.Fprintf(&b, "//   - %s (use As%s / %s)\n", v.t.Expr, v.label, ctors[i])
	}
	if n.Discriminator == nil || n.Discriminator.PropertyName == "" {
		b.WriteString("//\n// The union has no discriminator, so an As method succeeds whenever the JSON\n// fits the variant's shape; check the decoded fields to tell variants apart.\n")
	}
	fmt.Fprintf(&b, "type %s struct{ union }\n", name)
	for i, v := range vs {
		ctor := ctors[i]
		fmt.Fprintf(&b, "\n// As%s decodes the union as %s.\n", v.label, v.t.Expr)
		fmt.Fprintf(&b, "func (u %s) As%s() (%s, error) { return unionAs[%s](u.union) }\n", name, v.label, v.t.Expr, v.t.Expr)
		fmt.Fprintf(&b, "\n// %s returns a %s holding v.\n", ctor, name)
		fmt.Fprintf(&b, "func %s(v %s) *%s { return &%s{newUnion(v)} }\n", ctor, v.t.Expr, name, name)
	}
	if disc := n.Discriminator; disc != nil && disc.PropertyName != "" {
		prop := strconv.Quote(disc.PropertyName)
		fmt.Fprintf(&b, "\n// Discriminator returns the value of the %s property.\n", prop)
		fmt.Fprintf(&b, "func (u %s) Discriminator() (string, error) { return u.union.discriminator(%s) }\n", name, prop)

		byLabel := map[string][]string{}
		var order []string
		addCase := func(value, ref string) {
			for _, v := range vs {
				if v.ref == ref {
					if _, ok := byLabel[v.label]; !ok {
						order = append(order, v.label)
					}
					byLabel[v.label] = append(byLabel[v.label], strconv.Quote(value))
					return
				}
			}
		}
		if len(disc.Mapping) > 0 {
			keys := make([]string, 0, len(disc.Mapping))
			for k := range disc.Mapping {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				addCase(k, disc.Mapping[k])
			}
		} else {
			for _, v := range vs {
				if v.ref != "" {
					addCase(refName(v.ref), v.ref)
				}
			}
		}
		if len(order) > 0 {
			fmt.Fprintf(&b, "\n// ValueByDiscriminator decodes the union into the variant selected by the %s property.\n", prop)
			fmt.Fprintf(&b, "func (u %s) ValueByDiscriminator() (any, error) {\n", name)
			b.WriteString("\td, err := u.Discriminator()\n\tif err != nil {\n\t\treturn nil, err\n\t}\n\tswitch d {\n")
			for _, l := range order {
				fmt.Fprintf(&b, "\tcase %s:\n\t\treturn u.As%s()\n", strings.Join(byLabel[l], ", "), l)
			}
			fmt.Fprintf(&b, "\t}\n\treturn nil, unknownDiscriminator(%q, d)\n}\n", name)
		}
	}
	d.Code = b.String()
}
