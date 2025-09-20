package wire

import "encoding/json"

type Codec interface {
	Marshal(any) ([]byte, error)
	Unmarshal([]byte, any) error
	Name() string
}

type JSONCodec struct{}

func (j JSONCodec) Marshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

func (j JSONCodec) Unmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

func (j JSONCodec) Name() string {
	return "json"
}
