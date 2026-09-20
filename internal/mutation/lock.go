// Package mutation provides process-local locks shared by tag writes and file
// moves. Filesystem watchers and API handlers can observe the same path, so a
// move must not race an atomic tag replacement.
package mutation

import "sync"

var locks sync.Map

func Acquire(path string) func() {
	value, _ := locks.LoadOrStore(path, &sync.Mutex{})
	lock := value.(*sync.Mutex)
	lock.Lock()
	return lock.Unlock
}
