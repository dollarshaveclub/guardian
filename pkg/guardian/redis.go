package guardian

// NamespacedKey returns a key with the namespace prepended
func NamespacedKey(namespace, key string) string {
	return namespace + ":" + key
}
