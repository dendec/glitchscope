package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDeleteValidateEmpty(t *testing.T) {
	ds := newDeleteService(t.TempDir())
	if err := ds.Validate(""); !errors.Is(err, errDeleteEmpty) {
		t.Fatalf("Validate(\"\") = %v, want %v", err, errDeleteEmpty)
	}
}

func TestDeleteUnavailableBaseFailsLoudly(t *testing.T) {
	ds := newDeleteService(filepath.Join(t.TempDir(), "missing"))
	if err := ds.Validate(filepath.Join(ds.resolvedBase, "track.mp3")); err == nil {
		t.Fatal("unavailable library root should make deletion unavailable")
	}
}

func TestDeleteValidateDot(t *testing.T) {
	ds := newDeleteService(t.TempDir())
	if err := ds.Validate("."); !errors.Is(err, errDeleteDot) {
		t.Fatalf("Validate(\".\") = %v, want %v", err, errDeleteDot)
	}
}

func TestDeleteValidateRoot(t *testing.T) {
	dir := t.TempDir()
	ds := newDeleteService(dir)
	if err := ds.Validate(dir); !errors.Is(err, errDeleteRoot) {
		t.Fatalf("Validate(baseDir) = %v, want %v", err, errDeleteRoot)
	}
}

func TestDeleteValidateOutside(t *testing.T) {
	ds := newDeleteService(t.TempDir())
	if err := ds.Validate("/tmp/outside"); !errors.Is(err, errDeleteOutside) {
		t.Fatalf("Validate(outside) = %v, want %v", err, errDeleteOutside)
	}
}

func TestDeleteValidateSymlink(t *testing.T) {
	dir := t.TempDir()
	ds := newDeleteService(dir)

	target := filepath.Join(dir, "real.txt")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := ds.Validate(link); !errors.Is(err, errDeleteSymlink) {
		t.Fatalf("Validate(symlink) = %v, want %v", err, errDeleteSymlink)
	}
}

func TestDeleteValidateSymlinkOutside(t *testing.T) {
	dir := t.TempDir()
	ds := newDeleteService(dir)

	outside := filepath.Join(os.TempDir(), "outside_delete_test.txt")
	if err := os.WriteFile(outside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(outside)

	link := filepath.Join(dir, "link_out.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if err := ds.Validate(link); !errors.Is(err, errDeleteSymlink) {
		t.Fatalf("Validate(symlink outside) = %v, want %v", err, errDeleteSymlink)
	}
}

func TestDeleteDeleteFile(t *testing.T) {
	dir := t.TempDir()
	ds := newDeleteService(dir)

	f := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ds.Delete(f); err != nil {
		t.Fatalf("Delete(file) = %v", err)
	}
	if _, err := os.Stat(f); !os.IsNotExist(err) {
		t.Fatal("file should be deleted")
	}
}

func TestDeleteDeleteDir(t *testing.T) {
	dir := t.TempDir()
	ds := newDeleteService(dir)

	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(filepath.Join(sub, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "nested", "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "b.txt"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ds.Delete(sub); err != nil {
		t.Fatalf("Delete(dir) = %v", err)
	}
	if _, err := os.Stat(sub); !os.IsNotExist(err) {
		t.Fatal("directory should be deleted")
	}
}

func TestDeleteFileCount(t *testing.T) {
	dir := t.TempDir()
	ds := newDeleteService(dir)

	f := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	count, err := ds.FileCount(f)
	if err != nil || count != 1 {
		t.Fatalf("FileCount(file) = %d, %v, want 1, nil", count, err)
	}

	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		if err := os.WriteFile(filepath.Join(sub, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	count, err = ds.FileCount(sub)
	if err != nil || count != 3 {
		t.Fatalf("FileCount(dir) = %d, %v, want 3, nil", count, err)
	}
}

func TestDeleteOutsideRejected(t *testing.T) {
	dir := t.TempDir()
	ds := newDeleteService(dir)

	f := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ds.Delete(f); err != nil {
		t.Fatalf("Delete(valid) = %v", err)
	}

	outside := "/tmp/nope.txt"
	if err := ds.Delete(outside); !errors.Is(err, errDeleteOutside) {
		t.Fatalf("Delete(outside) = %v, want %v", err, errDeleteOutside)
	}
}
