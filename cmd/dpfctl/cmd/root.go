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

// version is the version passed by the main package.
var (
	version     string
	versionFlag bool
)

// TODO: can be integrated as a kubectl plugin
//
// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	// Version: "tbd",
	Use:   "dpfctl",
	Short: "A CLI tool for the DOCA Platform Framework",
	Long:  "Get useful information about the DOCA Platform Framework for debugging and troubleshooting.",
	RunE: func(cmd *cobra.Command, args []string) error {
		if versionFlag {
			return printVersion()
		}
		return cmd.Help()
	},
}

func init() {
	rootCmd.Flags().BoolVarP(&versionFlag, "version", "v", false, "Print the version of dpfctl")
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute(v string) {
	version = v
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func printVersion() error {
	// TODO: add server version.
	fmt.Printf("Client Version: %s\n", version)
	return nil
}
