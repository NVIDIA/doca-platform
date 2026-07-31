/*
Copyright 2024 NVIDIA

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package sync

import (
	"sync"
)

// NewIDLocker initialize and return new instance of IDLocker
func NewIDLocker() IDLocker {
	return &idLocker{
		locks: map[string]struct{}{},
	}
}

// UnlockFunc is returned by TryLock() to release the lock
type UnlockFunc func()

// IDLocker provides methods to lock object with provided ID
type IDLocker interface {
	// TryLock tries to lock provided id.
	// returns true and unlock function if lock on ID acquired,
	// returns false and nil if lock on ID is already held by a different caller
	TryLock(id string) (bool, UnlockFunc)
}

// idLocker is a default IDLocker implementation
type idLocker struct {
	internal sync.Mutex
	locks    map[string]struct{}
}

// TryLock is a IDLocker interface implementation for idLocker
func (nl *idLocker) TryLock(id string) (bool, UnlockFunc) {
	nl.internal.Lock()
	defer nl.internal.Unlock()
	_, exist := nl.locks[id]
	if exist {
		return false, nil
	}
	nl.locks[id] = struct{}{}
	return true, func() {
		nl.internal.Lock()
		defer nl.internal.Unlock()
		delete(nl.locks, id)
	}
}
