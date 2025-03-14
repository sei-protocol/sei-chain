root_dir="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

firehose_data_dir="$root_dir/.firehose-data"

fireeth="${FIREETH_BINARY:-fireeth}"
seid="${SEID_BINARY:-seid}"

check_fireeth() {
  if ! command -v "$fireeth" &> /dev/null; then
    echo "The '$fireeth' binary could not be found, you can install it through one of those means:"
    echo ""
    echo "- By running 'brew install streamingfast/tap/firehose-ethereum' on Mac or Linux system (with Homebrew installed)"
    echo "- By building it from source cloning https://github.com/streamingfast/firehose-ethereum.git and then 'go install ./cmd/fireeth'"
    echo "- By downloading a pre-compiled binary from https://github.com/streamingfast/firehose-ethereum/releases"
    exit 1
  fi
}

check_seid() {
  if ! command -v "$seid" &> /dev/null; then
    echo "The '$seid' binary could not be found, you can install it with:"
    echo ""
    echo "- go install github.com/streamingfast/sei-chain/cmd/seid@latest"
    exit 1
  fi
}

check_sd() {
  if ! command -v "sd" &> /dev/null; then
    echo "The 'sd' command is required for this script, please install it"
    echo "by following instructions at https://github.com/chmln/sd?tab=readme-ov-file#installation"
    exit 1
  fi
}
