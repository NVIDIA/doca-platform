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

//go:generate mockgen -copyright_file ../../../../../../hack/boilerplate.go.txt -destination mock/Utils.go -source mount.go

package mount

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"k8s.io/klog/v2"
	kmount "k8s.io/mount-utils"
)

// procMountInfoPath is the location of the mountinfo file
var procMountInfoPath = "/proc/self/mountinfo"

const (
	// DefaultMounter contains path to the mounter tool which should be used by default
	DefaultMounter = "/bin/mount"
)

// Utils is the interface provided by mount utils
type Utils interface {
	// Mount mounts source to target as fstype with given options.
	// options MUST not contain sensitive material (like passwords).
	Mount(source string, target string, fstype string, options []string) error
	// UnmountAndRemove unmounts given target if it is mounted and remove the target path
	UnmountAndRemove(target string) error
	// CheckMountExists check if the provided src is mounted to the provided mount point
	CheckMountExists(src, mountPoint string) (bool, kmount.MountInfo, error)
	// EnsureFileExist creates a file with specified path and all parent directories
	// if required
	EnsureFileExist(path string, mode os.FileMode) error
}

// New initialize and return instance of mount utils
func New(mounter kmount.Interface) Utils {
	return &mountUtils{
		mounter: mounter,
	}
}

type mountUtils struct {
	mounter kmount.Interface
}

// Mount is an Utils interface implementation for mountUtils
func (m *mountUtils) Mount(source string, target string, fstype string, options []string) error {
	return m.mounter.Mount(source, target, fstype, options)
}

// UnmountAndRemove is an Utils interface implementation for UnmountAndRemove
func (m *mountUtils) UnmountAndRemove(target string) error {
	isMount, err := m.isMountPoint(target)
	if err != nil {
		return err
	}
	if isMount {
		if err := m.mounter.Unmount(target); err != nil {
			return err
		}
	}
	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("failed to remove target path: %v", err)
	}
	return nil
}

// EnsureFileExist is an Utils interface implementation for EnsureFileExist
func (m *mountUtils) EnsureFileExist(path string, mode os.FileMode) error {
	exist, err := kmount.PathExists(path)
	if err != nil {
		return fmt.Errorf("failed to check stats for the file: %v", err)
	}
	if exist {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create dir: %v", err)
	}
	if err := os.WriteFile(path, []byte{}, mode); err != nil {
		return fmt.Errorf("failed to create file: %v", err)
	}
	return nil
}

// CheckMountExists is an Utils interface implementation for CheckMountExists
func (m *mountUtils) CheckMountExists(src, mountPoint string) (bool, kmount.MountInfo, error) {
	mounts, err := m.getMounts(src)
	if err != nil {
		return false, kmount.MountInfo{}, fmt.Errorf("failed to read mounts for src: %v", err)
	}
	for _, mnt := range mounts {
		if mnt.MountPoint == mountPoint {
			return true, mnt, nil
		}
	}
	return false, kmount.MountInfo{}, nil
}

// return list of valid mounts for the src
func (m *mountUtils) getMounts(src string) ([]kmount.MountInfo, error) {
	mounts, err := kmount.ParseMountInfo(procMountInfoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read mounts: %v", err)
	}
	result := make([]kmount.MountInfo, 0, len(mounts))
	for _, mnt := range mounts {
		if !isMountSourceMatch(src, mnt) {
			continue
		}
		exist, err := kmount.PathExists(mnt.MountPoint)
		if err != nil && !kmount.IsCorruptedMnt(err) {
			return nil, fmt.Errorf("failed to check mount: %v", err)
		}
		if !exist || kmount.IsCorruptedMnt(err) {
			klog.InfoS("invalid mount detected, ignore the mount",
				"src", src, "mountPoint", mnt.MountPoint)
			continue
		}
		result = append(result, mnt)
	}
	return result, nil
}

// src device for the mount point can be in a different fields in the mountInfo structure(depending on the mount type)
// this function knows which fields to check
func isMountSourceMatch(src string, mnt kmount.MountInfo) bool {
	if mnt.Source == src {
		return true
	}
	if mnt.Source == "udev" {
		// the kernel will add this suffix to mounts that point to a device that no longer exists.
		invalidMntSuffix := "//deleted"
		if strings.TrimSuffix(mnt.Root, invalidMntSuffix) == strings.TrimPrefix(src, "/dev") {
			if strings.HasSuffix(mnt.Root, invalidMntSuffix) {
				klog.InfoS("invalid mount detected", "source", mnt.Root)
				return false
			}
			return true
		}
	}
	return false
}

// check if provided path is mount point(something is mounted to this path)
func (m *mountUtils) isMountPoint(mountPoint string) (bool, error) {
	exist, err := kmount.PathExists(mountPoint)
	if err != nil {
		return false, fmt.Errorf("mount point check error: %v", err)
	}
	if !exist {
		return false, nil
	}
	mounts, err := kmount.ParseMountInfo(procMountInfoPath)
	if err != nil {
		return false, fmt.Errorf("failed to read mounts: %v", err)
	}
	for _, mnt := range mounts {
		if mnt.MountPoint == mountPoint {
			return true, nil
		}
	}
	return false, nil
}
