/*
Copyright 2026 NVIDIA

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

package netconfig

import "fmt"

// mockNMClient implements NMClient for testing.
type mockNMClient struct {
	version     string
	versionErr  error
	connections map[ConnectionPath]ConnectionSettings
	addedPaths  []ConnectionPath
	updatedMap  map[ConnectionPath]ConnectionSettings
	activated   []ConnectionPath

	listErr     error
	addErr      error
	updateErr   error
	activateErr error

	managingEthernet    bool
	managingEthernetErr error
}

func newMockNMClient() *mockNMClient {
	return &mockNMClient{
		version:     "1.42.0",
		connections: make(map[ConnectionPath]ConnectionSettings),
		updatedMap:  make(map[ConnectionPath]ConnectionSettings),
	}
}

func (m *mockNMClient) GetVersion() (string, error) {
	return m.version, m.versionErr
}

func (m *mockNMClient) ListConnections() ([]ConnectionPath, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	paths := make([]ConnectionPath, 0, len(m.connections))
	for p := range m.connections {
		paths = append(paths, p)
	}
	return paths, nil
}

func (m *mockNMClient) GetConnectionSettings(path ConnectionPath) (ConnectionSettings, error) {
	s, ok := m.connections[path]
	if !ok {
		return nil, fmt.Errorf("connection %s not found", path)
	}
	return s, nil
}

func (m *mockNMClient) AddConnection(settings ConnectionSettings) (ConnectionPath, error) {
	if m.addErr != nil {
		return "", m.addErr
	}
	path := ConnectionPath(fmt.Sprintf("/org/freedesktop/NetworkManager/Settings/%d", len(m.connections)+1))
	m.connections[path] = settings
	m.addedPaths = append(m.addedPaths, path)
	return path, nil
}

func (m *mockNMClient) UpdateConnection(path ConnectionPath, settings ConnectionSettings) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	m.connections[path] = settings
	m.updatedMap[path] = settings
	return nil
}

func (m *mockNMClient) ActivateConnection(path ConnectionPath) error {
	m.activated = append(m.activated, path)
	return m.activateErr
}

func (m *mockNMClient) IsManagingEthernetDevices() (bool, error) {
	return m.managingEthernet, m.managingEthernetErr
}

// addTestConnection adds a pre-existing connection to the mock.
func (m *mockNMClient) addTestConnection(path ConnectionPath, settings ConnectionSettings) {
	m.connections[path] = settings
}
