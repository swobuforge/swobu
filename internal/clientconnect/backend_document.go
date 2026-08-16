package clientconnect

import "encoding/json"

type jsonStringChange struct {
	path  keyPath
	value string
}

func setJSONStrings(editor jsonEditor, raw []byte, changes ...jsonStringChange) ([]byte, error) {
	next := raw
	var err error
	for _, change := range changes {
		next, err = editor.SetString(next, change.path, change.value)
		if err != nil {
			return nil, err
		}
	}
	return next, nil
}

func setJSONValue(editor jsonEditor, raw []byte, path keyPath, value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return editor.SetValue(raw, path, encoded)
}
