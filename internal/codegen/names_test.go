package main

import "testing"

func TestPascal(t *testing.T) {
	for in, want := range map[string]string{
		"vaultAccountId":         "VaultAccountID",
		"getVaultAccountAsset":   "GetVaultAccountAsset",
		"NFTs":                   "NFTs",
		"Api User":               "APIUser",
		"whitelist ip addresses": "WhitelistIPAddresses",
		"ETH_TEST6":              "EthTest6",
		"X-End-User-Wallet-Id":   "XEndUserWalletID",
		"tagIds":                 "TagIDs",
		"utxoSelectionParams":    "UTXOSelectionParams",
		"1inch":                  "N1inch",
		"":                       "",
	} {
		if got := pascal(in); got != want {
			t.Errorf("pascal(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCamel(t *testing.T) {
	for in, want := range map[string]string{
		"vaultAccountId": "vaultAccountID",
		"txId":           "txID",
		"ID":             "id",
		"URLPath":        "urlPath",
		"X-End-User":     "xEndUser",
	} {
		if got := camel(in); got != want {
			t.Errorf("camel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestServiceBaseName(t *testing.T) {
	for in, want := range map[string]string{
		"Blockchains & assets": "BlockchainsAndAssets",
		"Keys (Beta)":          "Keys",
		"Payments - Payout":    "PaymentsPayout",
	} {
		if got := serviceBaseName(in); got != want {
			t.Errorf("serviceBaseName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIdentSetUnique(t *testing.T) {
	s := identSet{}
	if a, b, c := s.unique("X"), s.unique("X"), s.unique("X"); a != "X" || b != "X2" || c != "X3" {
		t.Errorf("got %s %s %s", a, b, c)
	}
}

func TestSentence(t *testing.T) {
	for in, want := range map[string]string{
		"Get a vault account": "Get a vault account.",
		"Already done.":       "Already done.",
		"Drop ETH (EVM)":      "Drop ETH (EVM)",
		"":                    "",
		"Two\nlines":          "Two\nlines",
		"Is it supported?":    "Is it supported?",
		"Ends with `code`":    "Ends with `code`",
	} {
		if got := sentence(in); got != want {
			t.Errorf("sentence(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestOpBaseName(t *testing.T) {
	if got := opBaseName(&Operation{OperationID: "getVaultAccount"}); got != "GetVaultAccount" {
		t.Errorf("got %q", got)
	}
	if got := opBaseName(&Operation{Method: "GET", Path: "/vault/accounts/{vaultAccountId}"}); got != "GetVaultAccountsByVaultAccountID" {
		t.Errorf("fallback name = %q", got)
	}
}

func TestNormalizeAllOfKeepsRequired(t *testing.T) {
	g := newGen(&Spec{})
	s := &Schema{
		Required: []string{"b"},
		AllOf: []*Schema{
			{Type: "object", Required: []string{"a"}, Properties: Properties{
				{Name: "a", Schema: &Schema{Type: "string"}},
				{Name: "b", Schema: &Schema{Type: "string"}},
			}},
		},
	}
	n := g.normalize(s)
	req := map[string]bool{}
	for _, r := range n.Required {
		req[r] = true
	}
	if !req["a"] || !req["b"] {
		t.Errorf("required = %v, want a and b", n.Required)
	}
	if len(s.AllOf[0].Required) != 1 {
		t.Errorf("normalize mutated the input schema: %v", s.AllOf[0].Required)
	}
}
