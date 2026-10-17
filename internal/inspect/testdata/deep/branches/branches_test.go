package branches

import "testing"

// Covers the invalid-id and success paths of GetClient, but not ErrNotFound.
func TestGetClient(t *testing.T) {
	if GetClient(-1) != ErrInvalidID {
		t.Fatal("expected ErrInvalidID")
	}
	if GetClient(2) != nil {
		t.Fatal("expected success")
	}
}
