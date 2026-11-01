package hostos

// Adapter contains only facilities that actually vary by desktop OS.
type Adapter interface {
	DefaultStore() (string, error)
}
