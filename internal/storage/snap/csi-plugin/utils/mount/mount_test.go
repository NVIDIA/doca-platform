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

package mount

import (
	"fmt"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	kmount "k8s.io/mount-utils"
)

var mountFileContentTmpl = `7631 30 0:5 /loop0 %[1]s/stage/test1-stage rw,nosuid,relatime shared:2 - devtmpfs udev rw,size=65671776k,nr_inodes=16417944,mode=755,inode64
7633 30 0:5 /loop3//deleted %[1]s/stage/test3-stage rw,nosuid,relatime shared:2 - devtmpfs udev rw,size=65671776k,nr_inodes=16417944,mode=755,inode64
7698 30 0:5 /loop1 %[1]s/stage/test2-stage rw,nosuid,relatime shared:2 - devtmpfs udev rw,size=65671776k,nr_inodes=16417944,mode=755,inode64
7732 30 0:5 /loop0 %[1]s/block_publish/test1-publish rw,nosuid,relatime shared:2 - devtmpfs udev rw,size=65671776k,nr_inodes=16417944,mode=755,inode64
7731 30 7:1 / %[1]s/mount_publish/test2-publish rw,relatime shared:4031 - ext4 %[1]s/stage/test2-stage rw
`

func createFile(path string) {
	ExpectWithOffset(1, os.MkdirAll(filepath.Dir(path), 0744)).NotTo(HaveOccurred())
	ExpectWithOffset(1, os.WriteFile(path, []byte{}, 0644)).NotTo(HaveOccurred())
}

var _ = Describe("Mount Utils", func() {
	var (
		mountUtils Utils
		mounter    *kmount.FakeMounter
		tmpDir     string
		err        error
	)
	BeforeEach(func() {
		mounter = kmount.NewFakeMounter(nil)
		mountUtils = New(mounter)
		tmpDir, err = os.MkdirTemp("", "mount-utils-test*")
		Expect(err).NotTo(HaveOccurred())
		// resolve real path of the TMP dir, this is required to be compatible
		// with implementation of Unmount function from kmount.FakeMounter on macOS.
		// on mac the real path of the tmp dir is /private/tmp
		tmpDir, err = filepath.EvalSymlinks(tmpDir)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			Expect(os.RemoveAll(tmpDir)).NotTo(HaveOccurred())
		})
		origProcMountInfoPath := procMountInfoPath
		procMountInfoPath = filepath.Join(tmpDir, "mountinfo")
		DeferCleanup(func() {
			procMountInfoPath = origProcMountInfoPath
		})
	})

	Context("Mount", func() {
		It("Succeed", func() {
			Expect(mountUtils.Mount("/dev/sda", "/tmp/mount1", "", []string{"bind"})).NotTo(HaveOccurred())
			mounterLog := mounter.GetLog()
			Expect(mounterLog).To(HaveLen(1))
			Expect(mounterLog[0].Action).To(Equal("mount"))
			Expect(mounterLog[0].Source).To(Equal("/dev/sda"))
			Expect(mounterLog[0].Target).To(Equal("/tmp/mount1"))
		})
	})
	Context("UnmountAndRemove", func() {
		It("Succeed", func() {
			createFile(filepath.Join(tmpDir, "/stage/test1-stage"))
			Expect(os.WriteFile(procMountInfoPath, []byte(fmt.Sprintf(mountFileContentTmpl, tmpDir)), 0644)).NotTo(HaveOccurred())
			Expect(mountUtils.UnmountAndRemove(filepath.Join(tmpDir, "/stage/test1-stage"))).NotTo(HaveOccurred())
			mounterLog := mounter.GetLog()
			Expect(mounterLog).To(HaveLen(1))
			Expect(mounterLog[0].Action).To(Equal("unmount"))
			Expect(mounterLog[0].Target).To(Equal(filepath.Join(tmpDir, "/stage/test1-stage")))
			_, err := os.Stat(filepath.Join(tmpDir, "/stage/test1-stage"))
			Expect(os.IsNotExist(err)).To(BeTrue())
		})
		It("not mounted", func() {
			createFile(filepath.Join(tmpDir, "/stage/not-mounted"))
			Expect(os.WriteFile(procMountInfoPath, []byte(fmt.Sprintf(mountFileContentTmpl, tmpDir)), 0644)).NotTo(HaveOccurred())
			Expect(mountUtils.UnmountAndRemove(filepath.Join(tmpDir, "/stage/not-mounted"))).NotTo(HaveOccurred())
			mounterLog := mounter.GetLog()
			Expect(mounterLog).To(BeEmpty())
			_, err := os.Stat(filepath.Join(tmpDir, "/stage/not-mounted"))
			Expect(os.IsNotExist(err)).To(BeTrue())
		})
	})
	Context("EnsureFileExist", func() {
		It("Exist", func() {
			createFile(filepath.Join(tmpDir, "/stage/test1-stage"))
			Expect(mountUtils.EnsureFileExist(filepath.Join(tmpDir, "/stage/test1-stage"), 0644)).NotTo(HaveOccurred())
			_, err := os.Stat(filepath.Join(tmpDir, "/stage/test1-stage"))
			Expect(err).NotTo(HaveOccurred())
		})
		It("Created", func() {
			Expect(mountUtils.EnsureFileExist(filepath.Join(tmpDir, "/stage/test1-stage"), 0644)).NotTo(HaveOccurred())
			_, err := os.Stat(filepath.Join(tmpDir, "/stage/test1-stage"))
			Expect(err).NotTo(HaveOccurred())
		})
	})
	Context("CheckMountExists", func() {
		It("Bind mount", func() {
			createFile(filepath.Join(tmpDir, "/stage/test1-stage"))
			createFile(filepath.Join(tmpDir, "/block_publish/test1-publish"))
			Expect(os.WriteFile(procMountInfoPath, []byte(fmt.Sprintf(mountFileContentTmpl, tmpDir)), 0644)).NotTo(HaveOccurred())
			exist, _, err := mountUtils.CheckMountExists("/dev/loop0", filepath.Join(tmpDir, "/stage/test1-stage"))
			Expect(exist).To(BeTrue())
			Expect(err).NotTo(HaveOccurred())
		})
		It("FS Mount", func() {
			createFile(filepath.Join(tmpDir, "/stage/test2-stage"))
			Expect(os.MkdirAll(filepath.Join(tmpDir, "/mount_publish/test2-publish"), 0744)).NotTo(HaveOccurred())
			Expect(os.WriteFile(procMountInfoPath, []byte(fmt.Sprintf(mountFileContentTmpl, tmpDir)), 0644)).NotTo(HaveOccurred())
			exist, _, err := mountUtils.CheckMountExists(
				filepath.Join(tmpDir, "/stage/test2-stage"),
				filepath.Join(tmpDir, "/mount_publish/test2-publish"))
			Expect(exist).To(BeTrue())
			Expect(err).NotTo(HaveOccurred())
		})
		It("No target", func() {
			createFile(filepath.Join(tmpDir, "/stage/test2-stage"))
			Expect(os.WriteFile(procMountInfoPath, []byte(fmt.Sprintf(mountFileContentTmpl, tmpDir)), 0644)).NotTo(HaveOccurred())
			exist, _, err := mountUtils.CheckMountExists(
				filepath.Join(tmpDir, "/stage/test2-stage"),
				filepath.Join(tmpDir, "/mount_publish/test2-publish"))
			Expect(exist).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
		})
		It("Deleted source", func() {
			Expect(os.WriteFile(procMountInfoPath, []byte(fmt.Sprintf(mountFileContentTmpl, tmpDir)), 0644)).NotTo(HaveOccurred())
			exist, _, err := mountUtils.CheckMountExists(
				"/dev/loop3",
				filepath.Join(tmpDir, "/stage/test3-stage"))
			Expect(exist).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
