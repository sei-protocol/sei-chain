package bytes

import (
	"encoding/json"
	"fmt"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
)

// This is a trivial test for protobuf compatibility.
func TestMarshal(t *testing.T) {
	bz := []byte("hello world")
	dataB := HexBytes(bz)
	bz2, err := dataB.Marshal()
	assert.NoError(t, err)
	assert.Equal(t, bz, bz2)

	var dataB2 HexBytes
	err = (&dataB2).Unmarshal(bz)
	assert.NoError(t, err)
	assert.Equal(t, dataB, dataB2)
}

// Test that the hex encoding works.
func TestJSONMarshal(t *testing.T) {
	type TestStruct struct {
		B1 []byte
		B2 HexBytes
	}

	cases := []struct {
		input    []byte
		expected string
	}{
		{[]byte(``), `{"B1":"","B2":""}`},
		{[]byte(`a`), `{"B1":"YQ==","B2":"61"}`},
		{[]byte(`abc`), `{"B1":"YWJj","B2":"616263"}`},
		{[]byte("\x1a\x2b\x3c"), `{"B1":"Gis8","B2":"1A2B3C"}`},
	}

	for i, tc := range cases {
		tc := tc
		t.Run(fmt.Sprintf("Case %d", i), func(t *testing.T) {
			ts := TestStruct{B1: tc.input, B2: tc.input}

			// Test that it marshals correctly to JSON.
			jsonBytes, err := json.Marshal(ts)
			if err != nil {
				t.Fatal(err)
			}
			assert.Equal(t, string(jsonBytes), tc.expected)

			// TODO do fuzz testing to ensure that unmarshal fails

			// Test that unmarshaling works correctly.
			ts2 := TestStruct{}
			err = json.Unmarshal(jsonBytes, &ts2)
			if err != nil {
				t.Fatal(err)
			}
			assert.Equal(t, ts2.B1, tc.input)
			assert.Equal(t, string(ts2.B2), string(tc.input))
		})
	}
}

func TestHexBytes_String(t *testing.T) {
	hs := HexBytes([]byte("test me"))
	if _, err := strconv.ParseInt(hs.String(), 16, 64); err != nil {
		t.Fatal(err)
	}
}

// Define a struct to match the JSON structure
type ValidatorsHash struct {
	NextValidatorsHash HexBytes `json:"next_validators_hash"`
}

func TestMarshalBasic(t *testing.T) {
	var vh ValidatorsHash
	vh.NextValidatorsHash = []byte("abc")
	bz, err := json.Marshal(vh)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, string(bz), "{\"next_validators_hash\":\"616263\"}")
}

func TestUnmarshalBasic(t *testing.T) {
	jsonData := []byte(`{"next_validators_hash":"616263"}`)
	var vh ValidatorsHash
	err := json.Unmarshal(jsonData, &vh)
	if err != nil {
		t.Fatalf("Error unmarshalling JSON: %v", err)
		return
	}
	assert.Equal(t, string(vh.NextValidatorsHash), "abc")
}

func TestUnmarshalExample(t *testing.T) {
	jsonData := []byte(`{"next_validators_hash":"20021C2FB4B2DDFF6E8C484A2ED5862910E3AD7074FC6AD1C972AD34891AE3A4"}`)
	expectedLength := 32
	var vh ValidatorsHash
	err := json.Unmarshal(jsonData, &vh)
	if err != nil {
		t.Fatalf("Error unmarshalling JSON: %v", err)
		return
	}
	assert.Equal(t, expectedLength, len(vh.NextValidatorsHash))
}
