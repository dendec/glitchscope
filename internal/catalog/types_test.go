package catalog

import (
	"encoding/json"
	"testing"
)

func TestSourceKindString(t *testing.T) {
	tests := []struct {
		kind SourceKind
		want string
	}{
		{SourceLocal, "local"},
		{SourceModland, "modland"},
		{SourceModArchive, "modarchive"},
		{SourceKind(99), "SourceKind(99)"},
	}
	for _, tt := range tests {
		if got := tt.kind.String(); got != tt.want {
			t.Errorf("SourceKind(%d).String() = %q, want %q", int(tt.kind), got, tt.want)
		}
	}
}

func TestSourceKindMarshalUnmarshalJSON(t *testing.T) {
	for _, kind := range AllSourceKinds() {
		data, err := json.Marshal(kind)
		if err != nil {
			t.Fatalf("marshal %v: %v", kind, err)
		}
		var got SourceKind
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("unmarshal %s: %v", data, err)
		}
		if got != kind {
			t.Errorf("round-trip %v: got %v", kind, got)
		}
	}
}

func TestSourceKindLegacyInt(t *testing.T) {
	var got SourceKind
	if err := json.Unmarshal([]byte("0"), &got); err != nil {
		t.Fatal(err)
	}
	if got != SourceLocal {
		t.Fatalf("got %v, want SourceLocal", got)
	}
}

func TestSourceKindInvalid(t *testing.T) {
	var got SourceKind
	if err := json.Unmarshal([]byte(`"unknown"`), &got); err == nil {
		t.Fatal("expected error for unknown source kind")
	}
}

func TestDirectoryKeyComparable(t *testing.T) {
	k1 := DirectoryKey{Source: SourceLocal, Locator: "/music"}
	k2 := DirectoryKey{Source: SourceLocal, Locator: "/music"}
	k3 := DirectoryKey{Source: SourceModland, Locator: "/music"}

	if k1 != k2 {
		t.Fatal("equal keys should be comparable")
	}
	if k1 == k3 {
		t.Fatal("different sources should not be equal")
	}

	// Use as map key.
	m := map[DirectoryKey]string{k1: "found"}
	if m[k2] != "found" {
		t.Fatal("map lookup with equal key should succeed")
	}
}

func TestDirectoryKeyString(t *testing.T) {
	k := DirectoryKey{Source: SourceModland, Locator: "MODS/A"}
	if got := k.String(); got != "modland:MODS/A" {
		t.Fatalf("String() = %q, want %q", got, "modland:MODS/A")
	}
}

func TestAllSourceKinds(t *testing.T) {
	kinds := AllSourceKinds()
	if len(kinds) != 3 {
		t.Fatalf("len = %d, want 3", len(kinds))
	}
}
