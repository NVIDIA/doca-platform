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

import (
	"context"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
)

type FutureTaskState int

const (
	Poll FutureTaskState = iota
	Ready
)

type Future struct {
	mutex       sync.Mutex
	wg          sync.WaitGroup
	result      any
	err         error
	state       FutureTaskState
	cleanupFunc func() bool
}

func (f *Future) GetResult() (any, error) {
	f.wg.Wait()

	return f.result, f.err
}

func (f *Future) GetState() FutureTaskState {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	return f.state
}

func New(fn func() (any, error), cleanupFunc func() bool) *Future {

	f := Future{
		state:       Poll,
		err:         nil,
		cleanupFunc: cleanupFunc,
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

type TaskManager struct {
	sync.Mutex
	maxRun     int
	tasks      map[string]*Future
	taskRunCnt map[string]int
}

func NewTaskManager(maxRun int) *TaskManager {
	tm := &TaskManager{
		maxRun:     maxRun,
		tasks:      make(map[string]*Future),
		taskRunCnt: make(map[string]int),
	}
	tm.StartHousekeeping()
	return tm
}

func (m *TaskManager) StartHousekeeping() {
	go wait.PollUntilContextCancel(context.Background(), 10*time.Second, true, func(ctx context.Context) (bool, error) { //nolint:errcheck
		m.housekeeping()
		return false, nil
	})
}

func (m *TaskManager) RunTask(taskID string, f func() (any, error), cleanupFunc func() bool) (task *Future, maxReached bool) {
	m.Lock()
	defer m.Unlock()
	task, ok := m.tasks[taskID]
	if !ok {
		task = New(f, cleanupFunc)
		m.tasks[taskID] = task
		m.taskRunCnt[taskID] = 1
		return task, m.isMaxRunReached(taskID)
	}

	if task.GetState() != Ready {
		return task, m.isMaxRunReached(taskID)
	}

	_, err := task.GetResult()
	if err != nil {
		if m.isMaxRunReached(taskID) {
			return task, m.isMaxRunReached(taskID)
		}
		task = New(f, cleanupFunc)
		m.taskRunCnt[taskID]++
		m.tasks[taskID] = task
		return task, m.isMaxRunReached(taskID)
	}
	return task, m.isMaxRunReached(taskID)
}

func (m *TaskManager) Len() int {
	m.Lock()
	defer m.Unlock()
	return len(m.tasks)
}

func (m *TaskManager) housekeeping() {
	m.Lock()
	defer m.Unlock()
	for taskID, task := range m.tasks {
		if task.cleanupFunc == nil {
			continue
		}
		if task.cleanupFunc() {
			klog.Infof("delete task %s", taskID)
			delete(m.tasks, taskID)
			delete(m.taskRunCnt, taskID)
		}
	}
}

func (m *TaskManager) isMaxRunReached(taskID string) bool {
	return m.taskRunCnt[taskID] >= m.maxRun
}
