package configstore

// reencodeCurrentDocument exercises a complete ordinary publication while
// preserving the current document-owned receipts and audits.
func reencodeCurrentDocument(raw []byte) ([]byte, error) {
	store := &Store{}
	config, err := store.decodeDocument(raw)
	if err != nil {
		return nil, err
	}
	return encodeDocumentWithReceipts(config, store.schemaVersion, store.deletions, store.creations, store.creationAudits, store.deletionAudits, store.displayNameAudits)
}
