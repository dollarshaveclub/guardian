package guardian

import "testing"

func TestNamespacedKey(t *testing.T) {
	received := NamespacedKey("someNamespace", "someKey")
	expected := "someNamespace:someKey"

	if expected != received {
		t.Errorf("expected: %q received: %q", expected, received)
	}
}
