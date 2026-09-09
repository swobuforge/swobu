package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestArchiveIgnoresHostMetadata(t *testing.T) {
	for _, format := range []string{"tar.gz", "zip"} {
		t.Run(format, func(t *testing.T) {
			root := t.TempDir()
			source := filepath.Join(root, "swobu")
			if err := os.WriteFile(source, []byte("binary"), 0o600); err != nil {
				t.Fatal(err)
			}
			first := filepath.Join(root, "first."+format)
			if err := packageBinary(first, source); err != nil {
				t.Fatal(err)
			}
			when := time.Unix(1234567890, 0)
			if err := os.Chtimes(source, when, when); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(source, 0o755); err != nil {
				t.Fatal(err)
			}
			second := filepath.Join(root, "second."+format)
			if err := packageBinary(second, source); err != nil {
				t.Fatal(err)
			}
			firstBytes, err := os.ReadFile(first)
			if err != nil {
				t.Fatal(err)
			}
			secondBytes, err := os.ReadFile(second)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(firstBytes, secondBytes) {
				t.Fatal("archive changed with host timestamp or permissions")
			}
			if format == "zip" {
				reader, err := zip.NewReader(bytes.NewReader(firstBytes), int64(len(firstBytes)))
				if err != nil {
					t.Fatal(err)
				}
				if len(reader.File) != 1 || reader.File[0].Name != "swobu" || reader.File[0].Mode().Perm() != 0o755 {
					t.Fatal("unexpected ZIP entry")
				}
				entry, err := reader.File[0].Open()
				if err != nil {
					t.Fatal(err)
				}
				assertContents(t, entry)
				if err := entry.Close(); err != nil {
					t.Fatal(err)
				}
			} else {
				compressed, err := gzip.NewReader(bytes.NewReader(firstBytes))
				if err != nil {
					t.Fatal(err)
				}
				defer compressed.Close()
				reader := tar.NewReader(compressed)
				header, err := reader.Next()
				if err != nil {
					t.Fatal(err)
				}
				if header.Name != "swobu" || header.Mode != 0o755 || header.Uid != 0 || header.Gid != 0 || header.Uname != "" || header.Gname != "" || header.ModTime.Unix() != 0 {
					t.Fatalf("unexpected TAR metadata: %#v", header)
				}
				assertContents(t, reader)
				if _, err := reader.Next(); err != io.EOF {
					t.Fatal("unexpected extra archive entry")
				}
			}
		})
	}
}

func assertContents(t *testing.T, reader io.Reader) {
	t.Helper()
	content, err := io.ReadAll(reader)
	if err != nil || string(content) != "binary" {
		t.Fatalf("archive content=%q, error=%v", content, err)
	}
}

func TestRejectUnsafeSource(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "swobu")
	if err := os.Symlink(root, source); err != nil {
		t.Fatal(err)
	}
	if err := packageBinary(filepath.Join(root, "out.zip"), source); err == nil {
		t.Fatal("accepted source symlink")
	}
	if err := packageBinary(filepath.Join(root, "out.zip"), root); err == nil {
		t.Fatal("accepted directory")
	}
}
