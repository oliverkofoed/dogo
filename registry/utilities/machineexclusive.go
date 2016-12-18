package utilities

import "sync"

var l sync.RWMutex

func MachineExclusive(operation func() error) error {
	l.Lock()
	defer l.Unlock()
	return operation()
}
