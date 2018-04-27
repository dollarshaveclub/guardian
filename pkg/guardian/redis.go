package guardian

import (
	"fmt"
)

// NamespacedKey returns a key with the namespace prepended
func NamespacedKey(namespace, key string) string {
	return fmt.Sprintf("%v:%v", namespace, key)
}
