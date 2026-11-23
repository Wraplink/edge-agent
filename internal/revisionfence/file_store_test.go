package revisionfence

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFileStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fence.json")
	store := FileStore{Path: path}
	if _, err := store.Load(); !errors.Is(err, ErrFenceStateMissing) {
		t.Fatalf("missing state: %v", err)
	}
	if err := store.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	if err := store.Bootstrap(); !errors.Is(err, os.ErrExist) {
		t.Fatalf("duplicate bootstrap: %v", err)
	}
	empty, err := store.Load()
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty=%v err=%v", empty, err)
	}
	op := Operation{SubscriptionID: "sub", PublicIPID: "ip", Revision: 8, AttemptID: "attempt"}
	if err := store.Save(map[string]Operation{"sub": op}); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil || loaded["sub"] != op {
		t.Fatalf("loaded=%v err=%v", loaded, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("permissions: %v", info.Mode())
	}
}

func TestFileStoreRejectsCorruption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fence.json")
	store := FileStore{Path: path}
	for _, data := range []string{"{", "null", `{"sub":{"SubscriptionID":"other","PublicIPID":"ip","Revision":1,"AttemptID":"a"}}`} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Load(); err == nil {
			t.Fatalf("accepted corrupt data %q", data)
		}
	}
}

func TestFileStoreFailsClosedOnMissingDirectory(t *testing.T) {
	store := FileStore{Path: filepath.Join(t.TempDir(), "missing", "fence.json")}
	op := Operation{SubscriptionID: "sub", PublicIPID: "ip", Revision: 1, AttemptID: "a"}
	if err := store.Save(map[string]Operation{"sub": op}); err == nil {
		t.Fatal("unexpected success")
	}
	if err := store.Save(nil); !errors.Is(err, ErrInvalidOperation) {
		t.Fatalf("nil map: %v", err)
	}
}

func TestFileStoreLoadRejectsSymlinkWithoutReadingTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "external.json")
	content := []byte("{}")
	if err := os.WriteFile(target, content, 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "fence.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := (FileStore{Path: link}).Load(); err == nil {
		t.Fatal("symlinked revision fence must be rejected")
	}
	after, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(content) {
		t.Fatal("external state changed")
	}
}
