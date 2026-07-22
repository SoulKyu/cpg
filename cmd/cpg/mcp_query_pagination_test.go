package main

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testEnumArgs is a minimal args struct used only to exercise
// mustQuerySchema's Enum-patching mechanism in isolation, independent of any
// real tool's argument surface.
type testEnumArgs struct {
	Direction string `json:"direction,omitempty" jsonschema:"filter: ingress or egress"`
	Untouched string `json:"untouched,omitempty" jsonschema:"a field with no enum constraint"`
}

func TestMustQuerySchemaPatchesEnum(t *testing.T) {
	schema := mustQuerySchema[testEnumArgs](map[string][]any{
		"direction": {"ingress", "egress"},
	})

	require.Contains(t, schema.Properties, "direction")
	assert.Equal(t, []any{"ingress", "egress"}, schema.Properties["direction"].Enum)

	// A field not named in enumFields must be left unconstrained.
	require.Contains(t, schema.Properties, "untouched")
	assert.Empty(t, schema.Properties["untouched"].Enum)
}

func TestMustQuerySchemaPanicsOnUnknownField(t *testing.T) {
	assert.Panics(t, func() {
		mustQuerySchema[testEnumArgs](map[string][]any{"bogus_field": {"a", "b"}})
	}, "an enumFields key with no matching schema property must panic (build-time bug), never silently no-op")
}

func TestCursorRoundTrip(t *testing.T) {
	key := paginateBoundaryKey{Namespace: "prod", Workload: "api", Index: 3}
	token := encodeCursor(key)
	require.NotEmpty(t, token)

	got, err := decodeCursor(token)
	require.NoError(t, err)
	assert.Equal(t, key, got)
}

func TestDecodeCursorFailsClosedNeverPanics(t *testing.T) {
	t.Run("invalid base64", func(t *testing.T) {
		assert.NotPanics(t, func() {
			_, err := decodeCursor("!!!not-valid-base64!!!")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid cursor")
		})
	})

	t.Run("truncated payload", func(t *testing.T) {
		// Valid base64 alphabet, but decodes to a JSON fragment that is not
		// a complete/valid object — must still fail closed, not panic.
		truncated := "eyJucyI6InBy" // base64 of `{"ns":"pr` (missing closing brace)
		assert.NotPanics(t, func() {
			_, err := decodeCursor(truncated)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid cursor")
		})
	})

	t.Run("empty token", func(t *testing.T) {
		assert.NotPanics(t, func() {
			_, err := decodeCursor("")
			require.Error(t, err)
		})
	})
}

// testPaginateItem is a minimal item type carrying its own boundary key, for
// exercising paginate in isolation from any real query-tool result type.
type testPaginateItem struct {
	key paginateBoundaryKey
	val string
}

func testItemKeyOf(i testPaginateItem) paginateBoundaryKey { return i.key }

func buildTestPaginateItems(n int) []testPaginateItem {
	items := make([]testPaginateItem, n)
	for i := range items {
		items[i] = testPaginateItem{
			key: paginateBoundaryKey{Namespace: "ns", Workload: "wl", Index: i},
			val: fmt.Sprintf("item-%d", i),
		}
	}
	return items
}

func TestPaginateFirstMiddleLastPages(t *testing.T) {
	items := buildTestPaginateItems(5)

	// First page: no cursor, limit 2.
	page1, cursor1, hasMore1, total1 := paginate(items, testItemKeyOf, nil, 2, 50, 200)
	require.Len(t, page1, 2)
	assert.Equal(t, "item-0", page1[0].val)
	assert.Equal(t, "item-1", page1[1].val)
	assert.True(t, hasMore1)
	assert.NotEmpty(t, cursor1)
	assert.Equal(t, 5, total1)

	// Middle page: feed page1's cursor back in.
	after1, err := decodeCursor(cursor1)
	require.NoError(t, err)
	page2, cursor2, hasMore2, total2 := paginate(items, testItemKeyOf, &after1, 2, 50, 200)
	require.Len(t, page2, 2)
	assert.Equal(t, "item-2", page2[0].val)
	assert.Equal(t, "item-3", page2[1].val)
	assert.True(t, hasMore2)
	assert.NotEmpty(t, cursor2)
	assert.Equal(t, 5, total2)

	// Last page: feed page2's cursor back in — only 1 item remains.
	after2, err := decodeCursor(cursor2)
	require.NoError(t, err)
	page3, cursor3, hasMore3, total3 := paginate(items, testItemKeyOf, &after2, 2, 50, 200)
	require.Len(t, page3, 1)
	assert.Equal(t, "item-4", page3[0].val)
	assert.False(t, hasMore3)
	assert.Empty(t, cursor3)
	assert.Equal(t, 5, total3)
}

func TestPaginateEmptySet(t *testing.T) {
	page, cursor, hasMore, total := paginate([]testPaginateItem{}, testItemKeyOf, nil, 2, 50, 200)
	assert.Empty(t, page)
	assert.Empty(t, cursor)
	assert.False(t, hasMore)
	assert.Equal(t, 0, total)
}

func TestPaginateClampsLimit(t *testing.T) {
	items := buildTestPaginateItems(5)

	t.Run("zero limit clamps to default", func(t *testing.T) {
		page, _, _, _ := paginate(items, testItemKeyOf, nil, 0, 2, 4)
		assert.Len(t, page, 2)
	})

	t.Run("oversized limit clamps to max", func(t *testing.T) {
		page, _, hasMore, _ := paginate(items, testItemKeyOf, nil, 10000, 2, 4)
		assert.Len(t, page, 4)
		assert.True(t, hasMore, "5 items exist, max page size is 4 — one item remains")
	})
}

func TestClampLimit(t *testing.T) {
	assert.Equal(t, 20, clampLimit(0, 20, 100))
	assert.Equal(t, 20, clampLimit(-5, 20, 100))
	assert.Equal(t, 100, clampLimit(10000, 20, 100))
	assert.Equal(t, 30, clampLimit(30, 20, 100))
}
