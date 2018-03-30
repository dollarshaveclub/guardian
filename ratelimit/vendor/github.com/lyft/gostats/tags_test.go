package stats

import (
	"testing"
)

func TestSerializeTags(t *testing.T) {
	tags := map[string]string{"zzz": "hello", "q": "r"}
	serialized := serializeTags(tags)
	if serialized != ".__q=r.__zzz=hello" {
		t.Errorf("Serialized output (%s) didn't match expected output", serialized)
	}
}

func TestSerializeWithPerInstanceFlag(t *testing.T) {
	tags := map[string]string{"foo": "bar", "_f": "i"}
	serialized := serializeTags(tags)
	if serialized != ".___f=i.__foo=bar" {
		t.Errorf("Serialized output (%s) didn't match expected output", serialized)
	}
}