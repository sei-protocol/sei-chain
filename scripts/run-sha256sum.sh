#!/bin/bash

if ! command -v sha256sum &> /dev/null
then
    printf "sha256sum could not be found"
    exit
fi

SHA_DIR=$1


find $SHA_DIR -type f -exec sha256sum "{}" + > checksum.list
printf "Checksum of all files in $SHA_DIR:\n"
printf "\t%s\n" $(sha256sum checksum.list | awk '{print $1}')
printf "Please view checksum.list for individual files\n"
