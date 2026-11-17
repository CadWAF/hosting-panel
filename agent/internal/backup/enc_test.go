package backup

import (
	"encoding/json"
	"io"
)

func newEnc(w io.Writer) *json.Encoder { return json.NewEncoder(w) }
