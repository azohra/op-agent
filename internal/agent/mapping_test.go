package agent

import (
	"reflect"
	"strings"
	"testing"
)

func TestMappingForProfileAppliesNamedOverlay(t *testing.T) {
	values := map[string]string{
		"OP_AGENT_REFS":         "API_TOKEN op://vault/local/token\nSHARED op://vault/shared/value\n",
		"OP_AGENT_REFS_PROD_CA": "API_TOKEN op://vault/prod/token\n",
	}
	mapping, err := MappingForProfile("prod-ca", func(name string) string { return values[name] })
	if err != nil {
		t.Fatal(err)
	}
	want := Mapping{
		"API_TOKEN": "op://vault/prod/token",
		"SHARED":    "op://vault/shared/value",
	}
	if !reflect.DeepEqual(mapping, want) {
		t.Fatalf("mapping = %#v, want %#v", mapping, want)
	}
}

func TestMappingForProfileRequiresSelectedOverlay(t *testing.T) {
	values := map[string]string{"OP_AGENT_REFS": "TOKEN op://vault/local/token"}
	_, err := MappingForProfile("prod", func(name string) string { return values[name] })
	if err == nil || !strings.Contains(err.Error(), "OP_AGENT_REFS_PROD") {
		t.Fatalf("error = %v", err)
	}
}

func TestMappingForProfileRejectsAmbiguousNames(t *testing.T) {
	for _, profile := range []string{"Prod", "prod_ca", "-prod", strings.Repeat("a", 33)} {
		if _, err := MappingForProfile(profile, func(string) string { return "" }); err == nil {
			t.Fatalf("profile %q succeeded", profile)
		}
	}
}

func TestMappingForProfileUsesBaseWithoutAProfile(t *testing.T) {
	values := map[string]string{"OP_AGENT_REFS": "TOKEN op://vault/local/token"}
	mapping, err := MappingForProfile("", func(name string) string { return values[name] })
	if err != nil {
		t.Fatal(err)
	}
	if got := mapping["TOKEN"]; got != "op://vault/local/token" {
		t.Fatalf("TOKEN = %q", got)
	}
}

func TestParseMappingRejectsAmbiguousInput(t *testing.T) {
	tests := []string{
		"NOT-AN-ENV op://vault/item/value",
		"TOKEN not-a-reference",
		"TOKEN op://one\nTOKEN op://two",
		"TOKEN op://one extra",
	}
	for _, input := range tests {
		if _, err := ParseMapping(input); err == nil {
			t.Fatalf("ParseMapping(%q) succeeded", input)
		}
	}
}

func TestSelectMappingRequiresEveryKey(t *testing.T) {
	_, err := SelectMapping(Mapping{"ONE": "op://vault/item/one"}, []string{"ONE", "TWO"})
	if err == nil || !strings.Contains(err.Error(), "TWO") {
		t.Fatalf("error = %v", err)
	}
}

func TestSplitKeysAcceptsSpacesAndCommas(t *testing.T) {
	want := []string{"ONE", "TWO", "THREE"}
	if got := splitKeys("ONE TWO,THREE"); !reflect.DeepEqual(got, want) {
		t.Fatalf("splitKeys = %#v, want %#v", got, want)
	}
}
