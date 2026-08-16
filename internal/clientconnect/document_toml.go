package clientconnect

import (
	"bytes"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2/unstable"
)

// tomlEditor supports string leaves in the root or one concrete nested table.
type tomlEditor struct{}

type tomlStringLocation struct {
	value                  string
	exists                 bool
	start, end, firstTable int
	parentExists, insert   bool
	insertOffset           int
}

func (tomlEditor) String(raw []byte, path keyPath) (string, bool, error) {
	location, err := locateTOMLString(raw, path)
	return location.value, location.exists, err
}

func (tomlEditor) SetString(raw []byte, path keyPath, value string) ([]byte, error) {
	location, err := locateTOMLString(raw, path)
	if err != nil {
		return nil, err
	}
	encoded := strconv.Quote(value)
	if location.exists {
		out := append([]byte(nil), raw[:location.start]...)
		out = append(out, encoded...)
		out = append(out, raw[location.end:]...)
		return out, nil
	}
	if len(path) > 1 {
		line := path[len(path)-1] + " = " + encoded + "\n"
		if location.parentExists {
			offset := location.insertOffset
			out := append([]byte(nil), raw[:offset]...)
			out = append(out, []byte(line)...)
			out = append(out, raw[offset:]...)
			return out, nil
		}
		table := "[" + strings.Join(path[:len(path)-1], ".") + "]\n"
		out := append([]byte(nil), raw...)
		if len(out) > 0 && out[len(out)-1] != '\n' {
			out = append(out, '\n')
		}
		out = append(out, []byte(table+line)...)
		return out, nil
	}
	line := []byte(path[0] + " = " + encoded + "\n")
	offset := location.firstTable
	out := append([]byte(nil), raw[:offset]...)
	if offset > 0 && raw[offset-1] != '\n' {
		out = append(out, '\n')
	}
	out = append(out, line...)
	out = append(out, raw[offset:]...)
	return out, nil
}

func locateTOMLString(raw []byte, path keyPath) (tomlStringLocation, error) {
	if len(path) == 0 {
		return tomlStringLocation{}, fmt.Errorf("empty TOML path")
	}
	location := tomlStringLocation{firstTable: len(raw)}
	parser := unstable.Parser{}
	parser.Reset(raw)
	var currentTable []string
	parent := []string(path[:len(path)-1])
	for parser.NextExpression() {
		node := parser.Expression()
		if node.Kind == unstable.Table || node.Kind == unstable.ArrayTable {
			key := node.Child()
			lineStart := 0
			if key != nil {
				lineStart = bytes.LastIndexByte(raw[:int(key.Raw.Offset)], '\n') + 1
			}
			if location.parentExists && !location.insert {
				location.insert, location.insertOffset = true, lineStart
			}
			currentTable = currentTable[:0]
			keys := node.Key()
			for keys.Next() {
				currentTable = append(currentTable, string(keys.Node().Data))
			}
			if slices.Equal(currentTable, parent) {
				location.parentExists = true
				location.insert = false
				location.insertOffset = len(raw)
			}
			if key != nil {
				keyOffset := int(key.Raw.Offset)
				// The parser exposes the table key rather than a whole-expression
				// span. Its source line is nevertheless an unambiguous expression
				// boundary for both [table] and [[array-table]]; searching for a
				// bracket would split the latter between its two opening brackets.
				lineStart := bytes.LastIndexByte(raw[:keyOffset], '\n') + 1
				if lineStart < location.firstTable {
					location.firstTable = lineStart
				}
			}
			continue
		}
		if node.Kind != unstable.KeyValue {
			continue
		}
		keys := node.Key()
		var parts []string
		for keys.Next() {
			parts = append(parts, string(keys.Node().Data))
		}
		full := append([]string(nil), currentTable...)
		full = append(full, parts...)
		if !slices.Equal(full, []string(path)) {
			continue
		}
		if location.exists {
			return tomlStringLocation{}, fmt.Errorf("%s is ambiguous", joinKeyPath(path))
		}
		value := node.Value()
		if value.Kind != unstable.String {
			return tomlStringLocation{}, fmt.Errorf("%s is not a string", joinKeyPath(path))
		}
		location.exists = true
		location.value = string(value.Data)
		location.start = int(value.Raw.Offset)
		location.end = location.start + int(value.Raw.Length)
	}
	if err := parser.Error(); err != nil {
		return tomlStringLocation{}, fmt.Errorf("document is not valid TOML")
	}
	if location.parentExists && !location.insert {
		location.insertOffset = len(raw)
	}
	return location, nil
}
