//go:build !linux && !windows && !darwin

package watch

// newBackend reports that this platform has no event source yet. Callers keep
// polling, which is the same path taken when a watcher cannot be created for
// any other reason.
func newBackend() (backend, error) {
	return nil, ErrUnsupported
}
