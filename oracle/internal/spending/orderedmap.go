package spending

// OrderedMap is a map that iterates in insertion order, like a JS Map/Set.
// The TS oracle's Map/Set iteration order decides the token order sent in
// batchUpdate, so it is preserved exactly.
type OrderedMap[K comparable, V any] struct {
	keys []K
	vals map[K]V
}

// NewOrderedMap returns an empty OrderedMap.
func NewOrderedMap[K comparable, V any]() *OrderedMap[K, V] {
	return &OrderedMap[K, V]{vals: make(map[K]V)}
}

// Get returns the value for k.
func (m *OrderedMap[K, V]) Get(k K) (V, bool) {
	v, ok := m.vals[k]
	return v, ok
}

// Has reports whether k is present.
func (m *OrderedMap[K, V]) Has(k K) bool {
	_, ok := m.vals[k]
	return ok
}

// Set stores v under k; a new key is appended to the iteration order, an
// existing key keeps its position (JS Map semantics).
func (m *OrderedMap[K, V]) Set(k K, v V) {
	if _, ok := m.vals[k]; !ok {
		m.keys = append(m.keys, k)
	}
	m.vals[k] = v
}

// Keys returns the keys in insertion order.
func (m *OrderedMap[K, V]) Keys() []K { return append([]K(nil), m.keys...) }

// Len returns the number of entries.
func (m *OrderedMap[K, V]) Len() int { return len(m.keys) }
