package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCheckRejectsEditedDocumentWithoutWriting(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "openapi")
	build := exec.Command("go", "build", "-mod=readonly", "-buildvcs=false", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, output)
	}
	document := filepath.Join(root, "openapi.json")
	if output, err := exec.Command(binary, "-output", document).CombinedOutput(); err != nil {
		t.Fatalf("generate: %v %s", err, output)
	}
	if output, err := exec.Command(binary, "-check", "-output", document).CombinedOutput(); err != nil {
		t.Fatalf("fresh check: %v %s", err, output)
	}
	edited := []byte("{\"edited\":true}\n")
	if err := os.WriteFile(document, edited, 0644); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command(binary, "-check", "-output", document).Run(); err == nil {
		t.Fatal("drift check accepted edited output")
	}
	actual, err := os.ReadFile(document)
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != string(edited) {
		t.Fatal("check mutated edited input")
	}
}
