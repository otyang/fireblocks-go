package main

import (
	"strings"
	"unicode"
)

// initialisms follows the Go convention of keeping well-known abbreviations upper case.
var initialisms = map[string]bool{
	"ACL": true, "AML": true, "API": true, "ASCII": true, "CPU": true, "CSS": true, "CSV": true,
	"DNS": true, "EOF": true, "EVM": true, "GUID": true, "HTML": true, "HTTP": true, "HTTPS": true,
	"ID": true, "IP": true, "JSON": true, "JWT": true, "KYC": true, "MPC": true, "NCW": true,
	"NFT": true, "OTA": true, "OTP": true, "QPS": true, "RAM": true, "RPC": true, "SDK": true,
	"SLA": true, "SMTP": true, "SQL": true, "SSH": true, "TCP": true, "TLS": true, "TTL": true,
	"UDP": true, "UI": true, "UID": true, "URI": true, "URL": true, "UTF8": true, "UTXO": true,
	"UUID": true, "VASP": true, "XML": true, "XSRF": true, "XSS": true,
}

var goKeywords = map[string]bool{
	"break": true, "case": true, "chan": true, "const": true, "continue": true, "default": true,
	"defer": true, "else": true, "fallthrough": true, "for": true, "func": true, "go": true,
	"goto": true, "if": true, "import": true, "interface": true, "map": true, "package": true,
	"range": true, "return": true, "select": true, "struct": true, "switch": true, "type": true,
	"var": true,
}

func isAlnum(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// splitWords breaks an identifier-ish string into words on separators and case changes.
// A trailing lower-case "s" after an upper-case run stays attached ("NFTs", "IDs").
func splitWords(s string) []string {
	var words []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			words = append(words, string(cur))
			cur = nil
		}
	}
	rs := []rune(s)
	for i, r := range rs {
		if !isAlnum(r) || r > unicode.MaxASCII {
			flush()
			continue
		}
		if len(cur) > 0 {
			prev := rs[i-1]
			switch {
			case unicode.IsUpper(r) && (unicode.IsLower(prev) || unicode.IsDigit(prev)):
				flush()
			case unicode.IsUpper(r) && unicode.IsUpper(prev) && i+1 < len(rs) && unicode.IsLower(rs[i+1]):
				pluralS := rs[i+1] == 's' && (i+2 == len(rs) || !unicode.IsLower(rs[i+2]))
				if !pluralS {
					flush()
				}
			}
		}
		cur = append(cur, r)
	}
	flush()
	return words
}

func pascalWord(w string) string {
	up := strings.ToUpper(w)
	if initialisms[up] {
		return up
	}
	if len(w) > 1 && strings.HasSuffix(w, "s") && initialisms[up[:len(up)-1]] {
		return up[:len(up)-1] + "s"
	}
	return strings.ToUpper(w[:1]) + strings.ToLower(w[1:])
}

// pascal converts s into an exported Go identifier.
func pascal(s string) string {
	var b strings.Builder
	for _, w := range splitWords(s) {
		b.WriteString(pascalWord(w))
	}
	out := b.String()
	if out == "" {
		return ""
	}
	if unicode.IsDigit(rune(out[0])) {
		out = "N" + out
	}
	return out
}

// camel converts s into an unexported Go identifier.
func camel(s string) string {
	words := splitWords(s)
	if len(words) == 0 {
		return "v"
	}
	var b strings.Builder
	for i, w := range words {
		if i == 0 {
			b.WriteString(strings.ToLower(w))
			continue
		}
		b.WriteString(pascalWord(w))
	}
	out := b.String()
	if unicode.IsDigit(rune(out[0])) {
		out = "v" + out
	}
	return out
}

// snake converts s into a lower_snake_case file name fragment.
func snake(s string) string {
	words := splitWords(s)
	for i, w := range words {
		words[i] = strings.ToLower(w)
	}
	return strings.Join(words, "_")
}

// identSet hands out unique identifiers.
type identSet map[string]bool

func (s identSet) unique(name string) string {
	if !s[name] {
		s[name] = true
		return name
	}
	for i := 2; ; i++ {
		n := name + itoa(i)
		if !s[n] {
			s[n] = true
			return n
		}
	}
}

func itoa(i int) string {
	const digits = "0123456789"
	if i < 10 {
		return digits[i : i+1]
	}
	return itoa(i/10) + digits[i%10:i%10+1]
}
