package keys

import (
	"fmt"
	"io"

	yaml "gopkg.in/yaml.v2"

	cryptokeyring "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keyring"
)

// available output formats.
const (
	OutputFormatText = "text"
	OutputFormatJSON = "json"

	// defaultKeyDBName is the client's subdirectory where keys are stored.
	defaultKeyDBName = "keys"
)

type bechKeyOutFn func(keyInfo cryptokeyring.Info) (cryptokeyring.KeyOutput, error)

func printKeyInfo(w io.Writer, keyInfo cryptokeyring.Info, bechKeyOut bechKeyOutFn, output string) {
	ko, err := bechKeyOut(keyInfo)
	if err != nil {
		panic(err)
	}
	ko, err = cryptokeyring.PopulateEvmAddrIfApplicable(keyInfo, ko)
	if err != nil {
		panic(err)
	}

	switch output {
	case OutputFormatText:
		printTextInfos(w, []cryptokeyring.KeyOutput{ko})

	case OutputFormatJSON:
		out, err := KeysCdc.MarshalAsJSON(ko)
		if err != nil {
			panic(err)
		}

		_, _ = fmt.Fprintln(w, string(out))
	}
}

func printInfos(w io.Writer, infos []cryptokeyring.Info, output string) {
	kos, err := cryptokeyring.MkAccKeysOutput(infos)
	if err != nil {
		panic(err)
	}

	switch output {
	case OutputFormatText:
		printTextInfos(w, kos)

	case OutputFormatJSON:
		out, err := KeysCdc.MarshalAsJSON(kos)
		if err != nil {
			panic(err)
		}

		_, _ = fmt.Fprintf(w, "%s", out)
	}
}

func printTextInfos(w io.Writer, kos []cryptokeyring.KeyOutput) {
	out, err := yaml.Marshal(&kos)
	if err != nil {
		panic(err)
	}
	_, _ = fmt.Fprintln(w, string(out))
}
