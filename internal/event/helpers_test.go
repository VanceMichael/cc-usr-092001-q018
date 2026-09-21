package event

import (
	"encoding/json"
	"testing"
)

func jsonRaw(s string) json.RawMessage { return json.RawMessage(s) }

func mustDigest(t *testing.T, s string) string {
	t.Helper()
	d, err := CanonicalDigest([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return d
}
