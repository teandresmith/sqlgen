package cache

import (
	"encoding/json"
	"fmt"
)

// Serializer encodes and decodes cached entities. The interface matches
// encoding/json's Marshal / Unmarshal signatures so stdlib JSON works as
// the default without adaptation.
type Serializer interface {
	Marshal(v any) ([]byte, error)
	Unmarshal(data []byte, v any) error
}

// JSONSerializer is the default Serializer. It delegates to encoding/json
// and is the stdlib-only choice shipped with the runtime package.
type JSONSerializer struct{}

// Marshal encodes v to JSON.
func (JSONSerializer) Marshal(v any) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("cache json marshal: %w", err)
	}
	return data, nil
}

// Unmarshal decodes JSON data into v.
func (JSONSerializer) Unmarshal(data []byte, v any) error {
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("cache json unmarshal: %w", err)
	}
	return nil
}
