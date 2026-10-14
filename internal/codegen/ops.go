package main

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Service is the set of operations sharing a tag.
type Service struct {
	Tag    string
	GoName string // e.g. "Vaults"
	Type   string // e.g. "VaultsService"
	File   string
	Ops    []*Op
	Code   strings.Builder
}

// ParamDoc describes one parameter for the generated documentation.
type ParamDoc struct {
	Name, In, Type, Description string
	Required                    bool
}

// Op is a generated method.
type Op struct {
	*Operation
	GoName     string
	Signature  string
	Params     []ParamDoc
	BodyType   string
	ResultType string
}

// handledParams are header parameters exposed as request options instead of arguments.
var handledParams = map[string]bool{"idempotency-key": true}

var localNames = []string{
	"ctx", "body", "params", "opts", "s", "r", "out", "err", "http", "context",
}

func serviceBaseName(tag string) string {
	t := strings.ReplaceAll(tag, "&", " and ")
	t = strings.TrimSpace(strings.Replace(t, "(Beta)", "", 1))
	return pascal(t)
}

func (g *Gen) buildServices(includeInternal bool) []*Service {
	byTag := map[string]*Service{}
	var tags []string
	for _, op := range g.spec.Paths.Operations {
		if op.Internal && !includeInternal {
			continue
		}
		tag := "Default"
		if len(op.Tags) > 0 {
			tag = op.Tags[0]
		}
		svc, ok := byTag[tag]
		if !ok {
			svc = &Service{Tag: tag}
			byTag[tag] = svc
			tags = append(tags, tag)
		}
		svc.Ops = append(svc.Ops, &Op{Operation: op})
	}
	sort.Strings(tags)

	counts := map[string]int{}
	for _, t := range tags {
		counts[serviceBaseName(t)]++
	}
	var out []*Service
	for _, t := range tags {
		svc := byTag[t]
		name := serviceBaseName(t)
		if counts[name] > 1 {
			name = pascal(strings.ReplaceAll(t, "&", " and "))
		}
		svc.GoName = name
		svc.Type = g.idents.unique(name + "Service")
		svc.File = "service_" + snake(name) + ".gen.go"
		out = append(out, svc)
	}
	return out
}

func (g *Gen) resolveParam(p *Parameter) *Parameter {
	if p.Ref == "" {
		return p
	}
	if rp := g.spec.Components.Parameters[strings.TrimPrefix(p.Ref, "#/components/parameters/")]; rp != nil {
		return rp
	}
	return p
}

func (g *Gen) resolveResponse(r *Response) *Response {
	if r == nil || r.Ref == "" {
		return r
	}
	return g.spec.Components.Responses[strings.TrimPrefix(r.Ref, "#/components/responses/")]
}

var pathParamRE = regexp.MustCompile(`\{([^}]+)\}`)

type argInfo struct {
	name  string
	param *Parameter
	t     TRef
}

func (g *Gen) genService(svc *Service) {
	g.file = svc.File
	methodNames := identSet{}
	for _, op := range svc.Ops {
		op.GoName = methodNames.unique(opBaseName(op.Operation))
		g.genOp(svc, op)
	}
}

// opBaseName names an operation after its operationId, falling back to the
// method and path when the spec omits one.
func opBaseName(op *Operation) string {
	if n := pascal(op.OperationID); n != "" {
		return n
	}
	return pascal(strings.ToLower(op.Method) + " " + pathParamRE.ReplaceAllString(op.Path, "by $1"))
}

func (g *Gen) resolveRequestBody(rb *RequestBody) *RequestBody {
	if rb == nil || rb.Ref == "" {
		return rb
	}
	return g.spec.Components.RequestBodies[strings.TrimPrefix(rb.Ref, "#/components/requestBodies/")]
}

func (g *Gen) genOp(svc *Service, op *Op) {
	opName := opBaseName(op.Operation)

	// Merge path-item and operation parameters; the operation wins.
	var params []*Parameter
	index := map[string]int{}
	for _, list := range [][]*Parameter{op.PathItemParam, op.Parameters} {
		for _, p := range list {
			p = g.resolveParam(p)
			key := p.In + ":" + p.Name
			if i, ok := index[key]; ok {
				params[i] = p
				continue
			}
			index[key] = len(params)
			params = append(params, p)
		}
	}

	locals := identSet{}
	for _, n := range localNames {
		locals[n] = true
	}
	argName := func(raw string) string {
		n := camel(raw)
		if goKeywords[n] || locals[n] {
			n += "Param"
		}
		return locals.unique(n)
	}
	paramType := func(p *Parameter) TRef {
		t := g.typeOf(p.Schema, opName+pascal(p.Name))
		if p.In != "path" && (t.Kind == KStruct || t.Kind == KMap) {
			// Object-valued query parameters are passed pre-serialized.
			return TRef{"string", KScalar}
		}
		return t
	}

	var pathArgs, reqArgs, optArgs []argInfo
	byPathName := map[string]*Parameter{}
	for _, p := range params {
		if p.In == "path" {
			byPathName[p.Name] = p
		}
	}
	for _, m := range pathParamRE.FindAllStringSubmatch(op.Path, -1) {
		p := byPathName[m[1]]
		if p == nil {
			p = &Parameter{Name: m[1], In: "path", Required: true, Schema: &Schema{Type: "string"}}
		}
		pathArgs = append(pathArgs, argInfo{name: argName(p.Name), param: p, t: paramType(p)})
	}
	for _, p := range params {
		if p.In == "path" || p.In == "cookie" || (p.In == "header" && handledParams[strings.ToLower(p.Name)]) {
			continue
		}
		if p.Required {
			reqArgs = append(reqArgs, argInfo{name: argName(p.Name), param: p, t: paramType(p)})
		} else {
			optArgs = append(optArgs, argInfo{param: p, t: paramType(p)})
		}
	}

	// Request body.
	var bodyType TRef
	hasBody, multipart := false, false
	if rb := g.resolveRequestBody(op.RequestBody); rb != nil {
		if mt, ok := rb.Content["application/json"]; ok {
			hasBody = true
			bodyType = g.typeOf(mt.Schema, opName+"Request")
		} else if mt, ok := rb.Content["multipart/form-data"]; ok {
			hasBody, multipart = true, true
			bodyType = g.typeOf(mt.Schema, opName+"Request")
		}
	}
	bodyExpr := bodyType.Expr
	if hasBody && bodyType.Kind != KSlice && bodyType.Kind != KMap && bodyType.Kind != KAny {
		bodyExpr = "*" + bodyType.Expr
	}

	// Success response.
	var result TRef
	hasResult := false
	codes := append([]string{}, op.Responses.Codes...)
	sort.Strings(codes)
	for _, code := range codes {
		if len(code) != 3 || code[0] != '2' {
			continue
		}
		r := g.resolveResponse(op.Responses.ByCode[code])
		if r == nil {
			continue
		}
		mt, ok := r.Content["application/json"]
		if !ok {
			mt, ok = r.Content["*/*"]
		}
		if ok {
			result = g.typeOf(mt.Schema, opName+"Response")
			hasResult = true
			break
		}
	}
	resultExpr := result.Expr
	resultPtr := hasResult && (result.Kind == KStruct || result.Kind == KUnion)
	if resultPtr {
		resultExpr = "*" + result.Expr
	}

	// Optional parameters struct.
	paramsType := ""
	if len(optArgs) > 0 {
		paramsType = g.newType(opName + "Params")
		d := g.addDecl(paramsType)
		var b strings.Builder
		fmt.Fprintf(&b, "// %s holds the optional parameters of %s.%s.\n", paramsType, svc.Type, op.GoName)
		fmt.Fprintf(&b, "type %s struct {\n", paramsType)
		fields := identSet{}
		for i := range optArgs {
			a := &optArgs[i]
			fname := pascal(a.param.Name)
			if fname == "" {
				fname = "Param"
			}
			a.name = fields.unique(fname)
			expr := a.t.Expr
			if a.t.Kind != KSlice && a.t.Kind != KMap && a.t.Kind != KAny {
				expr = "*" + expr
			}
			lead := fmt.Sprintf("%s is the %q %s parameter.", a.name, a.param.Name, a.param.In)
			writeDoc(&b, "\t", lead, a.param.Description, a.param.Deprecated)
			fmt.Fprintf(&b, "\t%s %s\n", a.name, expr)
		}
		b.WriteString("}\n")
		d.Code = b.String()
	}

	// Signature.
	var sig []string
	sig = append(sig, "ctx context.Context")
	for _, a := range append(append([]argInfo{}, pathArgs...), reqArgs...) {
		sig = append(sig, a.name+" "+a.t.Expr)
	}
	if hasBody {
		sig = append(sig, "body "+bodyExpr)
	}
	if paramsType != "" {
		sig = append(sig, "params *"+paramsType)
	}
	sig = append(sig, "opts ...RequestOption")
	ret := "error"
	if hasResult {
		ret = "(" + resultExpr + ", error)"
	}
	op.Signature = fmt.Sprintf("func (s *%s) %s(%s) %s", svc.Type, op.GoName, strings.Join(sig, ", "), ret)

	// Docs metadata.
	for _, a := range append(append([]argInfo{}, pathArgs...), reqArgs...) {
		op.Params = append(op.Params, ParamDoc{Name: a.name, In: a.param.In, Type: a.t.Expr, Required: true, Description: a.param.Description})
	}
	for _, a := range optArgs {
		op.Params = append(op.Params, ParamDoc{Name: "params." + a.name, In: a.param.In, Type: a.t.Expr, Description: a.param.Description})
	}
	if hasBody {
		op.BodyType = bodyExpr
	}
	if hasResult {
		op.ResultType = resultExpr
	}

	// Method body.
	b := &svc.Code
	b.WriteString("\n")
	lead := fmt.Sprintf("%s calls %s %s.", op.GoName, op.Method, op.Path)
	desc := sentence(cleanText(op.Summary))
	if d := cleanText(op.Description); d != "" && d != cleanText(op.Summary) {
		if desc != "" {
			desc += "\n\n"
		}
		desc += d
	}
	writeDoc(b, "", lead, desc, op.Deprecated)
	fmt.Fprintf(b, "%s {\n", op.Signature)

	if len(pathArgs) == 0 {
		fmt.Fprintf(b, "\tr := newRequest(%s, %s)\n", methodConst(op.Method), strconv.Quote(op.Path))
	} else {
		fmt.Fprintf(b, "\tr := newRequest(%s, %s)\n", methodConst(op.Method), strconv.Quote(op.Path))
		var parts []string
		rest := op.Path
		for _, a := range pathArgs {
			ph := "{" + a.param.Name + "}"
			i := strings.Index(rest, ph)
			if i > 0 {
				parts = append(parts, strconv.Quote(rest[:i]))
			}
			parts = append(parts, fmt.Sprintf("r.pathParam(%q, %s)", a.param.Name, a.name))
			rest = rest[i+len(ph):]
		}
		if rest != "" {
			parts = append(parts, strconv.Quote(rest))
		}
		fmt.Fprintf(b, "\tr.path = %s\n", strings.Join(parts, " + "))
	}
	emitParam := func(p *Parameter, expr string, indent string) {
		switch p.In {
		case "query":
			fn := "queryValue"
			if p.Explode != nil && !*p.Explode {
				fn = "queryCSV"
			}
			fmt.Fprintf(b, "%sr.%s(%q, %s)\n", indent, fn, p.Name, expr)
		case "header":
			fmt.Fprintf(b, "%sr.header(%q, %s)\n", indent, p.Name, expr)
		}
	}
	for _, a := range reqArgs {
		emitParam(a.param, a.name, "\t")
	}
	if paramsType != "" {
		b.WriteString("\tif params != nil {\n")
		for _, a := range optArgs {
			emitParam(a.param, "params."+a.name, "\t\t")
		}
		b.WriteString("\t}\n")
	}
	if hasBody {
		field := "body"
		if multipart {
			field = "multipart"
		}
		fmt.Fprintf(b, "\tif body != nil {\n\t\tr.%s = body\n\t}\n", field)
	}
	switch {
	case !hasResult:
		b.WriteString("\treturn s.core.do(ctx, r, nil, opts)\n")
	case resultPtr:
		fmt.Fprintf(b, "\tvar out %s\n\tif err := s.core.do(ctx, r, &out, opts); err != nil {\n\t\treturn nil, err\n\t}\n\treturn &out, nil\n", result.Expr)
	default:
		fmt.Fprintf(b, "\tvar out %s\n\terr := s.core.do(ctx, r, &out, opts)\n\treturn out, err\n", result.Expr)
	}
	b.WriteString("}\n")
}

// sentence terminates a one-line summary with a period. Without it gofmt
// would turn the summary paragraph into a doc comment heading.
func sentence(s string) string {
	if s == "" || strings.Contains(s, "\n") {
		return s
	}
	switch s[len(s)-1] {
	case '.', '!', '?', ':', ')', '`':
		return s
	}
	return s + "."
}

func methodConst(m string) string {
	switch m {
	case "GET":
		return "http.MethodGet"
	case "POST":
		return "http.MethodPost"
	case "PUT":
		return "http.MethodPut"
	case "PATCH":
		return "http.MethodPatch"
	case "DELETE":
		return "http.MethodDelete"
	case "HEAD":
		return "http.MethodHead"
	case "OPTIONS":
		return "http.MethodOptions"
	}
	return strconv.Quote(m)
}
