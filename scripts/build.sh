#!/bin/bash

SEI_ROOT=$(git rev-parse --show-toplevel)

display_usage() {
  echo "This script support build or install seid."
  echo "Usage: ./build.sh [seid, seid-nitro]"
}

build_nitro_replayer() {
  NITRO_ROOT="$SEI_ROOT/../nitro-replayer/"
  if [ ! -d "${NITRO_ROOT}" ]; then
    cd "${SEI_ROOT}/../" || exit
    git clone https://github.com/sei-protocol/nitro-replayer
  fi
  cd "${NITRO_ROOT}" || exit
  cargo build --release
  cp ${NITRO_ROOT}/target/release/libnitro_replayer.dylib "${SEI_ROOT}/x/nitro/replay"
}

# Add helper to display usage
if [[ ( $1 == "--help") ||  $1 == "-h" ]]
then
  display_usage
  exit 0
fi

if [  $# -ne 1 ]
then
  display_usage
  exit 1
fi

# Take cmd input
OPTION=$1

# Display usage if input is a bad option
case $OPTION in
  "seid" )
    ;;
  "seid-nitro" )
    build_nitro_replayer
    ;;
  * )
   echo "Unrecognized option: $OPTION"
   display_usage
   exit 1
   ;;
esac

cd "$SEI_ROOT" || exit
go build -o ./build/seid ./cmd/seid
make install
