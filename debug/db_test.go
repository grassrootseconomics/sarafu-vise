package debug

import (
	"testing"

	storedb "git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"
	visedb "github.com/grassrootseconomics/go-vise/db"
)

func TestDebugDbSubKeyInfo(t *testing.T) {
	s := "foo"
	b := []byte{0x20}
	b = append(b, []byte{0x00, 0x02}...)
	r, err := ToKeyInfo(b, s)
	if err != nil {
		t.Fatal(err)
	}
	if r.SessionId != s {
		t.Fatalf("expected %s, got %s", s, r.SessionId)
	}
	if r.Typ != 32 {
		t.Fatalf("expected 64, got %d", r.Typ)
	}
	if r.SubTyp != 2 {
		t.Fatalf("expected 2, got %d", r.SubTyp)
	}
	if DebugCap&1 > 0 {
		if r.Label != "tracking id" {
			t.Fatalf("expected 'tracking id', got '%s'", r.Label)
		}
	}
}

func TestDebugDbKeyInfo(t *testing.T) {
	s := "bar"
	b := []byte{0x10}
	b = append(b, []byte(s)...)
	r, err := ToKeyInfo(b, s)
	if err != nil {
		t.Fatal(err)
	}
	if r.SessionId != s {
		t.Fatalf("expected %s, got %s", s, r.SessionId)
	}
	if r.Typ != 16 {
		t.Fatalf("expected 16, got %d", r.Typ)
	}
	if DebugCap&1 > 0 {
		if r.Label != "internal state" {
			t.Fatalf("expected 'internal_state', got '%s'", r.Label)
		}
	}
}

func TestDebugDbKeyInfoRestore(t *testing.T) {
	s := "bar"
	b := []byte{visedb.DATATYPE_USERDATA}
	k := storedb.ToBytes(storedb.DATA_ACTIVE_SYM)
	b = append(b, k...)

	r, err := ToKeyInfo(b, s)
	if err != nil {
		t.Fatal(err)
	}
	if r.SessionId != s {
		t.Fatalf("expected %s, got %s", s, r.SessionId)
	}
	if r.Typ != 32 {
		t.Fatalf("expected 32, got %d", r.Typ)
	}
	if DebugCap&1 > 0 {
		if r.Label != "active sym" {
			t.Fatalf("expected 'active sym', got '%s'", r.Label)
		}
	}
}
