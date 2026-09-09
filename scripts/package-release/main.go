package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: package-release ARCHIVE.tar.gz|ARCHIVE.zip BINARY")
		os.Exit(2)
	}
	if err := packageBinary(os.Args[1], os.Args[2]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func packageBinary(destination, sourcePath string) error {
	if !strings.HasSuffix(destination, ".tar.gz") && !strings.HasSuffix(destination, ".zip") {
		return fmt.Errorf("unsupported archive extension")
	}
	info, err := os.Lstat(sourcePath)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("source must be a regular file")
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".package-release-*")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	defer temporary.Close()
	if strings.HasSuffix(destination, ".zip") {
		err = writeZip(temporary, source, filepath.Base(sourcePath))
	} else {
		err = writeTar(temporary, source, filepath.Base(sourcePath), info.Size())
	}
	if err != nil {
		return err
	}
	if err := temporary.Chmod(0o644); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporary.Name(), destination)
}

func writeZip(output io.Writer, source io.Reader, name string) error {
	writer := zip.NewWriter(output)
	header := &zip.FileHeader{Name: name, Method: zip.Deflate}
	header.SetMode(0o755)
	header.SetModTime(time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC))
	entry, err := writer.CreateHeader(header)
	if err != nil {
		return err
	}
	if _, err := io.Copy(entry, source); err != nil {
		return err
	}
	return writer.Close()
}

func writeTar(output io.Writer, source io.Reader, name string, size int64) error {
	compressed := gzip.NewWriter(output)
	compressed.Header.OS = 255
	writer := tar.NewWriter(compressed)
	header := &tar.Header{
		Name: name, Mode: 0o755, Size: size,
		Typeflag: tar.TypeReg, ModTime: time.Unix(0, 0), Format: tar.FormatUSTAR,
	}
	if err := writer.WriteHeader(header); err != nil {
		return err
	}
	if _, err := io.Copy(writer, source); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return compressed.Close()
}
