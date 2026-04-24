package diff

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestYAMLDiffIdentical(t *testing.T) {
	d, err := UnifiedYAML("a.yaml", "b.yaml", []byte("key: value\n"), []byte("key: value\n"), false)
	assert.NoError(t, err)
	assert.Empty(t, d)
}

func TestYAMLDiffShowsAddition(t *testing.T) {
	old := []byte("foo: 1\n")
	updated := []byte("foo: 1\nbar: 2\n")
	d, err := UnifiedYAML("old", "new", old, updated, false)
	assert.NoError(t, err)
	assert.Contains(t, d, "+bar: 2")
}

func TestYAMLDiffShowsDeletion(t *testing.T) {
	old := []byte("foo: 1\nbar: 2\n")
	updated := []byte("foo: 1\n")
	d, err := UnifiedYAML("old", "new", old, updated, false)
	assert.NoError(t, err)
	assert.Contains(t, d, "-bar: 2")
}

func TestYAMLDiffHeaders(t *testing.T) {
	d, err := UnifiedYAML("a.yaml", "b.yaml", []byte("x: 1\n"), []byte("x: 2\n"), false)
	assert.NoError(t, err)
	assert.Contains(t, d, "--- a.yaml")
	assert.Contains(t, d, "+++ b.yaml")
}
