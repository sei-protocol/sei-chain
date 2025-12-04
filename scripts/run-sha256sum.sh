#!/bin/bash

# Check if 'sha256sum' command is available on the system
if ! command -v sha256sum &> /dev/null
then
    # If 'sha256sum' is not found, print an error message
    printf "sha256sum could not be found\n"
    # Exit the script with a non-zero status to indicate failure
    exit 1
fi

# Assign the first command-line argument to the variable SHA_DIR
SHA_DIR=$1

# Ensure that SHA_DIR is provided and is a directory
if [ -z "$SHA_DIR" ]; then
    printf "Usage: $0 <directory_path>\n"
    exit 1
elif [ ! -d "$SHA_DIR" ]; then
    printf "Error: $SHA_DIR is not a directory or does not exist.\n"
    exit 1
fi

# Use 'find' to locate all files within SHA_DIR
# For each found file, execute 'sha256sum' to calculate its SHA256 checksum
# Append all checksums to 'checksum.list'
find "$SHA_DIR" -type f -exec sha256sum "{}" + > checksum.list

# Print a header message indicating that checksums have been calculated
printf "Checksum of all files in %s:\n" "$SHA_DIR"

# Calculate the SHA256 checksum of the entire 'checksum.list' file
# Extract only the checksum value using 'awk' (ignoring the filename)
# Print the aggregated checksum with indentation for readability
printf "\t%s\n" "$(sha256sum checksum.list | awk '{print $1}')"

# Inform the user that individual file checksums are available in 'checksum.list'
printf "Please view checksum.list for individual file checksums.\n"
