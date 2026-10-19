package export

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// LayoutVersion is the version of the layout documents the Architecture
// view saves: the positions the user gave the cards, kept apart from the
// analysis (frontend/src/features/architecture). The document is the
// browser's to interpret; GitView only checks it is one and stores it.
const LayoutVersion = 1

// MaxLayoutSize bounds a layout document (thousands of cards fit easily).
const MaxLayoutSize = 8 << 20

// ParseLayout checks that data is a layout document: a JSON object with a
// supported layout_version. It returns the document compacted.
func ParseLayout(data []byte) (json.RawMessage, error) {
	if len(data) > MaxLayoutSize {
		return nil, fmt.Errorf("layout larger than %d MiB", MaxLayoutSize>>20)
	}
	var doc struct {
		Version *float64 `json:"layout_version"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("not a layout document: %w", err)
	}
	switch {
	case doc.Version == nil:
		return nil, errors.New("not a layout document: no layout_version")
	case *doc.Version != LayoutVersion:
		return nil, fmt.Errorf("unsupported layout_version %v (this GitView reads %d)", *doc.Version, LayoutVersion)
	}
	var out bytes.Buffer
	if err := json.Compact(&out, data); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
