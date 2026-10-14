package fireblocks

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/textproto"
	"reflect"
	"strconv"
	"strings"
)

// Ptr returns a pointer to v. It is handy for optional fields:
//
//	req := &fireblocks.TransactionRequest{AssetID: fireblocks.Ptr("BTC")}
func Ptr[T any](v T) *T { return &v }

// Deref returns the value p points to, or the zero value when p is nil. It is
// handy for reading optional response fields:
//
//	fmt.Println(fireblocks.Deref(account.Name))
func Deref[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}

// File is a file uploaded with a multipart/form-data request.
type File struct {
	// Name is the file name sent to the server.
	Name string
	// ContentType is the MIME type; it defaults to application/octet-stream.
	ContentType string
	// Content is read once when the request is sent.
	Content io.Reader
}

// union is the representation shared by every generated oneOf/anyOf type: it
// keeps the raw JSON so no information is lost, and decodes it on demand.
type union struct {
	raw json.RawMessage
	err error
}

var errEmptyUnion = errors.New("fireblocks: union value is empty")

func newUnion(v any) union {
	b, err := json.Marshal(v)
	return union{raw: b, err: err}
}

// MarshalJSON implements json.Marshaler.
func (u union) MarshalJSON() ([]byte, error) {
	if u.err != nil {
		return nil, u.err
	}
	if len(u.raw) == 0 {
		return []byte("null"), nil
	}
	return u.raw, nil
}

// UnmarshalJSON implements json.Unmarshaler.
func (u *union) UnmarshalJSON(b []byte) error {
	u.raw = append(json.RawMessage(nil), b...)
	u.err = nil
	return nil
}

// Raw returns the JSON encoding of the value held by the union.
func (u union) Raw() json.RawMessage { return u.raw }

func unionAs[T any](u union) (T, error) {
	var v T
	if u.err != nil {
		return v, u.err
	}
	if len(u.raw) == 0 {
		return v, errEmptyUnion
	}
	err := json.Unmarshal(u.raw, &v)
	return v, err
}

func (u union) discriminator(prop string) (string, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(u.raw, &m); err != nil {
		return "", fmt.Errorf("fireblocks: read discriminator %q: %w", prop, err)
	}
	raw, ok := m[prop]
	if !ok {
		return "", fmt.Errorf("fireblocks: discriminator %q not present", prop)
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", fmt.Errorf("fireblocks: discriminator %q is not a string", prop)
	}
	return s, nil
}

func unknownDiscriminator(typeName, value string) error {
	return fmt.Errorf("fireblocks: unknown %s discriminator value %q", typeName, value)
}

var jsonMarshalerType = reflect.TypeOf((*json.Marshaler)(nil)).Elem()

// formatValues renders a parameter value as strings: nil pointers and empty
// slices produce nothing, slices produce one string per element.
func formatValues(v any) []string {
	rv := reflect.ValueOf(v)
	for rv.IsValid() && rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	if !rv.IsValid() {
		return nil
	}
	if (rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array) && rv.Type().Elem().Kind() != reflect.Uint8 {
		out := make([]string, 0, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			out = append(out, formatReflect(rv.Index(i)))
		}
		return out
	}
	return []string{formatReflect(rv)}
}

// formatValue renders a single parameter value.
func formatValue(v any) string {
	vals := formatValues(v)
	if len(vals) == 0 {
		return ""
	}
	return strings.Join(vals, ",")
}

func formatReflect(rv reflect.Value) string {
	for rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return ""
		}
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.String:
		return rv.String()
	case reflect.Bool:
		return strconv.FormatBool(rv.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(rv.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(rv.Uint(), 10)
	case reflect.Float32:
		return strconv.FormatFloat(rv.Float(), 'f', -1, 32)
	case reflect.Float64:
		return strconv.FormatFloat(rv.Float(), 'f', -1, 64)
	}
	if rv.Type().Implements(jsonMarshalerType) || reflect.PointerTo(rv.Type()).Implements(jsonMarshalerType) {
		b, err := json.Marshal(rv.Interface())
		if err == nil {
			var s string
			if json.Unmarshal(b, &s) == nil {
				return s
			}
			return string(b)
		}
	}
	switch rv.Kind() {
	case reflect.Struct, reflect.Map, reflect.Slice, reflect.Array:
		// Objects and nested lists are sent as JSON.
		if b, err := json.Marshal(rv.Interface()); err == nil {
			return string(b)
		}
	}
	return fmt.Sprint(rv.Interface())
}

var quoteEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`)

// encodeMultipart encodes a generated request struct as multipart/form-data,
// using the json tag of each field as the form field name.
func encodeMultipart(v any) ([]byte, string, error) {
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil, "", errors.New("nil body")
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return nil, "", fmt.Errorf("unsupported multipart body %T", v)
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	rt := rv.Type()
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		name := strings.Split(f.Tag.Get("json"), ",")[0]
		if name == "" || name == "-" || !f.IsExported() {
			continue
		}
		fv := rv.Field(i).Interface()
		if file, ok := fv.(*File); ok {
			if file == nil {
				continue
			}
			if file.Content == nil {
				return nil, "", fmt.Errorf("file field %q has no content", name)
			}
			ct := file.ContentType
			if ct == "" {
				ct = "application/octet-stream"
			}
			h := textproto.MIMEHeader{}
			h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, quoteEscaper.Replace(name), quoteEscaper.Replace(file.Name)))
			h.Set("Content-Type", ct)
			part, err := mw.CreatePart(h)
			if err != nil {
				return nil, "", err
			}
			if _, err := io.Copy(part, file.Content); err != nil {
				return nil, "", err
			}
			continue
		}
		for _, s := range formatValues(fv) {
			if err := mw.WriteField(name, s); err != nil {
				return nil, "", err
			}
		}
	}
	if err := mw.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), mw.FormDataContentType(), nil
}
