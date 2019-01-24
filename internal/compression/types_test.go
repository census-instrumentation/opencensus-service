package compression

import (
	"testing"
)

func TestIsSupportedCompressionType(t *testing.T) {
	if !IsSupportedCompressionType("gzip") {
		t.Error("gzip is marked as supported but returned unsupported")
	}

	if IsSupportedCompressionType("badType") {
		t.Error("badType is not supported but was returned as supported")
	}
}
