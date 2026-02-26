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

import (
	"fmt"

	"github.com/godbus/dbus/v5"
)

const (
	nmService       = "org.freedesktop.NetworkManager"
	nmPath          = "/org/freedesktop/NetworkManager"
	nmSettingsPath  = "/org/freedesktop/NetworkManager/Settings"
	nmSettingsIface = "org.freedesktop.NetworkManager.Settings"
	nmConnIface     = "org.freedesktop.NetworkManager.Settings.Connection"
	dbusPropsIface  = "org.freedesktop.DBus.Properties"
)

// ConnectionPath is a D-Bus object path identifying a NetworkManager connection profile.
type ConnectionPath = dbus.ObjectPath

// ConnectionSettings maps D-Bus section names to their key-value properties.
type ConnectionSettings = map[string]map[string]dbus.Variant

// NMClient abstracts NetworkManager D-Bus operations for testability.
// The real implementation uses dbus.SystemBus() which returns a shared,
// process-global connection. Callers should NOT close it.
type NMClient interface {
	// GetVersion returns the NetworkManager version string.
	GetVersion() (string, error)
	// ListConnections returns all connection profile paths.
	ListConnections() ([]ConnectionPath, error)
	// GetConnectionSettings reads the settings of a connection profile.
	GetConnectionSettings(path ConnectionPath) (ConnectionSettings, error)
	// AddConnection creates a new connection profile and returns its path.
	AddConnection(settings ConnectionSettings) (ConnectionPath, error)
	// UpdateConnection replaces the settings of an existing connection profile.
	UpdateConnection(path ConnectionPath, settings ConnectionSettings) error
	// ActivateConnection activates a connection, auto-selecting the device.
	ActivateConnection(path ConnectionPath) error
	// IsManagingEthernetDevices returns true if NetworkManager is actively
	// managing at least one ethernet device.
	IsManagingEthernetDevices() (bool, error)
}

// dbusNMClient implements NMClient using a real D-Bus system bus connection.
type dbusNMClient struct {
	conn *dbus.Conn
}

// NewDBusNMClient creates a new NMClient backed by the system D-Bus.
func NewDBusNMClient() (NMClient, error) {
	conn, err := dbus.SystemBus()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to system D-Bus: %w", err)
	}
	return &dbusNMClient{conn: conn}, nil
}

func (c *dbusNMClient) GetVersion() (string, error) {
	obj := c.conn.Object(nmService, dbus.ObjectPath(nmPath))
	var version string
	err := obj.Call(dbusPropsIface+".Get", 0, nmService, "Version").Store(&version)
	if err != nil {
		return "", fmt.Errorf("failed to get NetworkManager version: %w", err)
	}
	return version, nil
}

func (c *dbusNMClient) ListConnections() ([]ConnectionPath, error) {
	obj := c.conn.Object(nmService, dbus.ObjectPath(nmSettingsPath))
	var paths []ConnectionPath
	err := obj.Call(nmSettingsIface+".ListConnections", 0).Store(&paths)
	if err != nil {
		return nil, fmt.Errorf("failed to list connections: %w", err)
	}
	return paths, nil
}

func (c *dbusNMClient) GetConnectionSettings(path ConnectionPath) (ConnectionSettings, error) {
	obj := c.conn.Object(nmService, path)
	var settings ConnectionSettings
	err := obj.Call(nmConnIface+".GetSettings", 0).Store(&settings)
	if err != nil {
		return nil, fmt.Errorf("failed to get settings for %s: %w", path, err)
	}
	return settings, nil
}

func (c *dbusNMClient) AddConnection(settings ConnectionSettings) (ConnectionPath, error) {
	obj := c.conn.Object(nmService, dbus.ObjectPath(nmSettingsPath))
	var connPath ConnectionPath
	err := obj.Call(nmSettingsIface+".AddConnection", 0, settings).Store(&connPath)
	if err != nil {
		return "", fmt.Errorf("failed to add connection: %w", err)
	}
	return connPath, nil
}

func (c *dbusNMClient) UpdateConnection(path ConnectionPath, settings ConnectionSettings) error {
	obj := c.conn.Object(nmService, path)
	err := obj.Call(nmConnIface+".Update", 0, settings).Err
	if err != nil {
		return fmt.Errorf("failed to update connection %s: %w", path, err)
	}
	return nil
}

func (c *dbusNMClient) ActivateConnection(path ConnectionPath) error {
	obj := c.conn.Object(nmService, dbus.ObjectPath(nmPath))
	var activeConnPath dbus.ObjectPath
	err := obj.Call(nmService+".ActivateConnection", 0,
		path, dbus.ObjectPath("/"), dbus.ObjectPath("/")).Store(&activeConnPath)
	if err != nil {
		return fmt.Errorf("failed to activate connection %s: %w", path, err)
	}
	return nil
}

// nmDeviceTypeEthernet is NM_DEVICE_TYPE_ETHERNET per
// https://networkmanager.dev/docs/api/latest/nm-dbus-types.html#NMDeviceType
const nmDeviceTypeEthernet uint32 = 1

const nmDeviceIface = "org.freedesktop.NetworkManager.Device"

func (c *dbusNMClient) IsManagingEthernetDevices() (bool, error) {
	nmObj := c.conn.Object(nmService, dbus.ObjectPath(nmPath))
	variant, err := nmObj.GetProperty(nmService + ".Devices")
	if err != nil {
		return false, fmt.Errorf("failed to get NM Devices property: %w", err)
	}
	devPaths, ok := variant.Value().([]dbus.ObjectPath)
	if !ok {
		return false, fmt.Errorf("unexpected Devices property type: %T", variant.Value())
	}

	for _, dp := range devPaths {
		devObj := c.conn.Object(nmService, dp)

		dtVar, err := devObj.GetProperty(nmDeviceIface + ".DeviceType")
		if err != nil {
			continue
		}
		devType, ok := dtVar.Value().(uint32)
		if !ok || devType != nmDeviceTypeEthernet {
			continue
		}

		mVar, err := devObj.GetProperty(nmDeviceIface + ".Managed")
		if err != nil {
			continue
		}
		if managed, ok := mVar.Value().(bool); ok && managed {
			return true, nil
		}
	}
	return false, nil
}
