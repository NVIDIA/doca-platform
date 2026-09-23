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

package artifact

import (
	"fmt"
	"io"
	"os"

	diskfs "github.com/diskfs/go-diskfs"
)

// MaxSeedISOSize bounds the cidata seed.iso kept in memory. The controller writes three small text
// files into it; the ISO9660 container itself is a few hundred KiB.
const MaxSeedISOSize = 16 << 20

// UserDataFromSeedISO reads /user-data out of the cidata ISO9660 image the controller generates for
// BF4 (util.MkIso, volume label "cidata"). go-diskfs needs a file, so the bytes are staged at path.
func UserDataFromSeedISO(iso []byte, path string) ([]byte, error) {
	if err := os.WriteFile(path, iso, 0o600); err != nil {
		return nil, fmt.Errorf("stage seed.iso: %w", err)
	}
	return UserDataFromSeedISOFile(path)
}

// UserDataFromSeedISOFile reads /user-data out of a cidata ISO9660 image file.
func UserDataFromSeedISOFile(path string) ([]byte, error) {
	disk, err := diskfs.Open(path, diskfs.WithOpenMode(diskfs.ReadOnly))
	if err != nil {
		return nil, fmt.Errorf("open seed.iso: %w", err)
	}
	defer func() { _ = disk.Close() }()
	fs, err := disk.GetFilesystem(0)
	if err != nil {
		return nil, fmt.Errorf("read seed.iso filesystem: %w", err)
	}
	f, err := fs.OpenFile("/user-data", os.O_RDONLY)
	if err != nil {
		return nil, fmt.Errorf("open /user-data in seed.iso: %w", err)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("read /user-data from seed.iso: %w", err)
	}
	return data, nil
}
