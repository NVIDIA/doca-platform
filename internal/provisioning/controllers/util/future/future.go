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

package future

import "sync"

type FurtureTaskState int

const (
	Poll FurtureTaskState = iota
	Ready
)

type Future struct {
	mutex  sync.Mutex
	wg     sync.WaitGroup
	result any
	err    error
	state  FurtureTaskState
}

func (f *Future) GetResult() (any, error) {
	f.wg.Wait()

	return f.result, f.err
}

func (f *Future) GetState() FurtureTaskState {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	return f.state
}

func New(fn func() (any, error)) *Future {

	f := Future{
		state: Poll,
		err:   nil,
	}
	f.wg.Add(1)

	go func() {
		f.result, f.err = fn()
		f.mutex.Lock()
		f.state = Ready
		f.mutex.Unlock()
		f.wg.Done()
	}()

	return &f
}
