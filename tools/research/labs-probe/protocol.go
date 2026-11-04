package main

import (
	"bufio"
	"errors"
	"io"

	"github.com/devigned/vx-bridge/internal/protocol"
)

const (
	receiveBudget  = protocol.MaxFrame
	maxDepth       = protocol.MaxDepth
	acknowledgment = `{"Code":200,"Message":"Shot received successfully","Player":null}`
)

var (
	errBudget  = errors.New("receive_budget_exhausted")
	errFraming = protocol.ErrFraming
	errShape   = protocol.ErrShape
)

// The historical probe budget includes read-ahead and inter-object whitespace.
type budgetReader struct {
	reader io.Reader
	read   int
	limit  int
}

// Read fills p within the remaining total-input budget, counting all bytes
// including read-ahead and whitespace. Reaching the limit returns errBudget
// on the next read, not a clean EOF.
func (r *budgetReader) Read(p []byte) (int, error) {
	if r.read == r.limit {
		return 0, errBudget
	}
	if len(p) > r.limit-r.read {
		p = p[:r.limit-r.read]
	}
	n, err := r.reader.Read(p)
	r.read += n
	return n, err
}

type objectStream struct{ reader *bufio.Reader }

func (s *objectStream) next() ([]byte, error) {
	frame, err := (&protocol.Stream{Reader: s.reader}).Next()
	// Preserve v3's historical EOF-only truncation mapping. The persistent
	// pilot additionally retains incomplete-frame context on other I/O errors.
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		parts := joined.Unwrap()
		if len(parts) == 2 && parts[0] == errFraming {
			if errors.Is(parts[1], io.EOF) {
				return nil, errFraming
			}
			return nil, parts[1]
		}
	}
	return frame, err
}

type sourceFlags = protocol.Flags

type message struct {
	id     string
	flags  sourceFlags
	fields []string
	kind   string
}

func inspect(frame []byte) (message, error) {
	msg, err := protocol.Inspect(frame)
	return message{msg.ID, msg.Flags, msg.Fields, msg.Kind}, err
}

func strictJSON(frame []byte) bool { return protocol.StrictJSON(frame) }
