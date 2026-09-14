package target_config

import "testing"

func TestFileCredentialInputAuthorsOpaqueDaemonPath(t *testing.T) {
	var applied string
	row := newCredentialField(CredentialFieldProps{Apply: func(ref string) { applied = ref }})
	row.saveFile(`  /home/operator/.config/provider.key  `)
	if applied != "file:/home/operator/.config/provider.key" {
		t.Fatalf("credential reference = %q", applied)
	}
}

func TestFileCredentialInputPrefillsExistingOpaqueDaemonPath(t *testing.T) {
	const path = "/home/operator/.config/provider.key"
	row := newCredentialField(CredentialFieldProps{Ref: "file:" + path})
	row.stage.Set(credStageMenu)
	row.openFile()
	if got := row.filePath.Get(); got != path {
		t.Fatalf("file input = %q, want %q", got, path)
	}
}

func TestFileCredentialInputDoesNotPrefillAnotherReferenceKind(t *testing.T) {
	row := newCredentialField(CredentialFieldProps{Ref: "env:PROVIDER_KEY"})
	row.stage.Set(credStageMenu)
	row.openFile()
	if got := row.filePath.Get(); got != "" {
		t.Fatalf("file input = %q, want empty", got)
	}
}
