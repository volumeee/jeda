package strutil

import (
	"strings"
	"testing"
)

func TestGenerateRandomKey(t *testing.T) {
	prefix := "jd_test"
	key1 := GenerateRandomKey(prefix)
	key2 := GenerateRandomKey(prefix)

	if !strings.HasPrefix(key1, "jd_test_") {
		t.Errorf("Expected prefix 'jd_test_', got: %s", key1)
	}

	if key1 == key2 {
		t.Errorf("Random keys should be unique, got two indentical: %s", key1)
	}
}
