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
	"os"

	"github.com/spf13/cobra"
)

// describeCmd represents the describe command
var describeAllCmd = &cobra.Command{
	Use:     "all",
	Short:   "Describe all DPF resources",
	Long:    "Describe the overall status of DPF resources in your cluster.",
	Example: fmt.Sprintf(exampleCmds, rootCmd.Root().Name(), "all"),
	Run: func(cmd *cobra.Command, args []string) {
		if err := runDescribe(cmd, "all"); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	},
}

func init() {
	describeCmd.AddCommand(describeAllCmd)
	describeAllCmd.Flags().AddFlagSet(describeCmd.Flags())
}
