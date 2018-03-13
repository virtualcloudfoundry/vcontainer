package helpers

type Empty struct{}

// sets.String is a set of strings, implemented via map[string]struct{} for minimal memory consumption.
type String map[string]Empty

// New creates a String from a list of values.
func NewString(items ...string) String {
	ss := String{}
	ss.Insert(items...)
	return ss
}

// Insert adds items to the set.
func (s String) Insert(items ...string) {
	for _, item := range items {
		s[item] = Empty{}
	}
}
