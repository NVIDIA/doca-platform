/*
Copyright 2025 NVIDIA

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

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// describeStorageCmd represents the describe storage command
var describeStorageCmd = &cobra.Command{
	Use:     "storage",
	Short:   "Describe DPF storage resources.",
	Long:    "Describe the storage resources in your cluster.",
	Example: fmt.Sprintf(exampleCmds, rootCmd.Root().Name(), "storage"),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDescribe("storage")
	},
}

func init() {
	describeCmd.AddCommand(describeStorageCmd)
	describeStorageCmd.Flags().AddFlagSet(describeCmd.Flags())
}
