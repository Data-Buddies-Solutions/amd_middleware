package safelog

import (
	"bytes"
	"encoding/json"
	"io"
	"sync"
)

type writer struct {
	destination io.Writer
	mu          sync.Mutex
}

func NewWriter(destination io.Writer) io.Writer {
	return &writer{destination: destination}
}

func (w *writer) Write(input []byte) (int, error) {
	entry := bytes.TrimSpace(input)
	if len(entry) == 0 {
		return len(input), nil
	}
	if entry[0] != '{' || !json.Valid(entry) {
		encoded, err := json.Marshal(struct {
			Message string `json:"message"`
		}{Message: string(entry)})
		if err != nil {
			return 0, err
		}
		entry = encoded
	}
	line := append(append([]byte(nil), entry...), '\n')

	w.mu.Lock()
	defer w.mu.Unlock()
	written, err := w.destination.Write(line)
	if err != nil {
		return 0, err
	}
	if written != len(line) {
		return 0, io.ErrShortWrite
	}
	return len(input), nil
}
