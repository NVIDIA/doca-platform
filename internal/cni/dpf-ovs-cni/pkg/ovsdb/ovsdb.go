//go:build linux

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

package ovsdb

import (
	"context"
	"fmt"

	"github.com/nvidia/doca-platform/pkg/ovsmodel"
	"github.com/nvidia/doca-platform/pkg/ovsutils"

	"github.com/ovn-org/libovsdb/client"
)

// Connector connects to OVSDB.
//
//go:generate mockgen -build_constraint linux -copyright_file ../../../../../hack/boilerplate.go.txt -destination mock/ovsdb.go -source ovsdb.go
type Connector interface {
	Connect(ctx context.Context, ovsSocket string) (ovsutils.API, error)
}

// DefaultConnector connects to the live OVSDB socket.
type DefaultConnector struct{}

var _ Connector = DefaultConnector{}

func (DefaultConnector) Connect(ctx context.Context, ovsSocket string) (ovsutils.API, error) {
	if ovsSocket == "" {
		ovsSocket = "unix:/var/run/openvswitch/db.sock"
	}

	dbmodel, err := ovsmodel.FullDatabaseModel()
	if err != nil {
		return nil, fmt.Errorf("unable to create DB model error: %v", err)
	}

	ovsDB, err := client.NewOVSDBClient(dbmodel, client.WithEndpoint(ovsSocket))
	if err != nil {
		return nil, fmt.Errorf("unable to create DB client error: %v", err)
	}
	if err := ovsDB.Connect(ctx); err != nil {
		return nil, fmt.Errorf("failed to connect to ovsdb socket %s: %v", ovsSocket, err)
	}
	if _, err := ovsDB.MonitorAll(ctx); err != nil {
		return nil, fmt.Errorf("failed to monitor ovsdb: %v", err)
	}

	return &ovsutils.Client{Client: ovsDB}, nil
}
