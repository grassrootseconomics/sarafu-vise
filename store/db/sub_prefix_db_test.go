package db

import (
	"bytes"
	"context"
	"testing"

	memdb "git.defalsify.org/vise.git/db/mem"
)

func TestSubPrefix(t *testing.T) {
	ctx := context.Background()
	db := memdb.NewMemDb()
	err := db.Connect(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	sdba := NewSubPrefixDb(db, []byte("tinkywinky"))
	err = sdba.Put(ctx, []byte("foo"), []byte("dipsy"))
	if err != nil {
		t.Fatal(err)
	}

	r, err := sdba.Get(ctx, []byte("foo"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(r, []byte("dipsy")) {
		t.Fatalf("expected 'dipsy', got %s", r)
	}

	sdbb := NewSubPrefixDb(db, []byte("lala"))
	r, err = sdbb.Get(ctx, []byte("foo"))
	if err == nil {
		t.Fatal("expected not found")
	}

	err = sdbb.Put(ctx, []byte("foo"), []byte("pu"))
	if err != nil {
		t.Fatal(err)
	}
	r, err = sdbb.Get(ctx, []byte("foo"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(r, []byte("pu")) {
		t.Fatalf("expected 'pu', got %s", r)
	}

	r, err = sdba.Get(ctx, []byte("foo"))
	if !bytes.Equal(r, []byte("dipsy")) {
		t.Fatalf("expected 'dipsy', got %s", r)
	}
}
