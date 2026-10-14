#!/bin/bash

#  2025 NVIDIA CORPORATION & AFFILIATES
#
#  Licensed under the Apache License, Version 2.0 (the License);
#  you may not use this file except in compliance with the License.
#  You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
#  Unless required by applicable law or agreed to in writing, software
#  distributed under the License is distributed on an AS IS BASIS,
#  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#  See the License for the specific language governing permissions and
#  limitations under the License.

if [ "$#" -ne 2 ]; then
	echo "Usage: $0 <header_file> <target_directory>"
	exit 1
fi

header_file="$1"
target_dir="$2"

if [ ! -f "$header_file" ]; then
	echo "Error: Header file '$header_file' not found."
	exit 1
fi

if [ ! -d "$target_dir" ]; then
	echo "Error: Target directory '$target_dir' not found."
	exit 1
fi

# Extract the copyright line from the header file
copyright_line=$(grep "Copyright" "$header_file")

for file in "$target_dir"/*.go; do
	if [ -f "$file" ]; then
		# Check if the copyright line exists in the file
		if grep -q "$copyright_line" "$file"; then
			echo "Header already exists in $file. Skipping."
		else
			temp_file=$(mktemp)
			{
				cat "$header_file"
				echo
				cat "$file"
			} > "$temp_file"
			mv "$temp_file" "$file" 2> /dev/null
			echo "Added header to $file"
		fi
	fi
done

echo "Header addition complete."
