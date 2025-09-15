package main

import "github.com/go-faster/jx"

// JSONEncode implements json encoder.
func (m Message) JSONEncode(e *jx.Encoder) {
	m.Encode(e)
}
