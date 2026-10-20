package mutation

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func sampleMutations() []Mutation {
	return []Mutation{
		{Key: []byte("insert"), NewValue: []byte("new")},
		{Key: []byte("update"), PreviousValue: []byte("old"), NewValue: []byte("new")},
		{Key: []byte("delete"), PreviousValue: []byte("old")},
		{Key: []byte("unknown"), PreviousValueUnknown: true, NewValue: []byte("new")},
		{Key: []byte{}, PreviousValue: []byte{}, NewValue: []byte{}},
	}
}

func TestMutationRoundTrip(t *testing.T) {
	tests := []struct {
		name     string
		mutation Mutation
	}{
		{
			name:     "insert",
			mutation: Mutation{Key: []byte("key"), NewValue: []byte("new")},
		},
		{
			name:     "update",
			mutation: Mutation{Key: []byte("key"), PreviousValue: []byte("old"), NewValue: []byte("new")},
		},
		{
			name:     "delete",
			mutation: Mutation{Key: []byte("key"), PreviousValue: []byte("old")},
		},
		{
			name:     "delete of absent key",
			mutation: Mutation{Key: []byte("key")},
		},
		{
			name:     "previous value unknown",
			mutation: Mutation{Key: []byte("key"), PreviousValueUnknown: true, NewValue: []byte("new")},
		},
		{
			name:     "empty key",
			mutation: Mutation{Key: []byte{}, NewValue: []byte("new")},
		},
		{
			name:     "present but empty values",
			mutation: Mutation{Key: []byte("key"), PreviousValue: []byte{}, NewValue: []byte{}},
		},
		{
			name: "large values",
			mutation: Mutation{
				Key:           bytes.Repeat([]byte{0xab}, 1000),
				PreviousValue: bytes.Repeat([]byte{0xcd}, 100_000),
				NewValue:      bytes.Repeat([]byte{0xef}, 70_000),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := tt.mutation.Serialize()
			decoded, consumed, err := DeserializeMutation(data)
			require.NoError(t, err)
			require.Equal(t, tt.mutation, *decoded)
			require.Equal(t, len(data), consumed)
		})
	}
}

func TestMutationSerializedLayout(t *testing.T) {
	mutation := Mutation{Key: []byte("k"), PreviousValue: []byte("pv"), NewValue: []byte("nv")}
	expected := []byte{
		0,          // version
		0, 0, 0, 1, // key length
		'k',
		0,          // previous value unknown
		1,          // previous value present
		0, 0, 0, 2, // previous value length
		'p', 'v',
		1,          // new value present
		0, 0, 0, 2, // new value length
		'n', 'v',
	}
	require.Equal(t, expected, mutation.Serialize())
}

func TestDeserializeMutationIgnoresTrailingData(t *testing.T) {
	mutation := Mutation{Key: []byte("key"), PreviousValue: []byte("old"), NewValue: []byte("new")}
	serialized := mutation.Serialize()
	data := append(bytes.Clone(serialized), 0xff, 0xff, 0xff)

	decoded, consumed, err := DeserializeMutation(data)
	require.NoError(t, err)
	require.Equal(t, mutation, *decoded)
	require.Equal(t, len(serialized), consumed)
}

func TestDeserializedMutationDoesNotAliasInput(t *testing.T) {
	mutation := Mutation{Key: []byte("key"), PreviousValue: []byte("old"), NewValue: []byte("new")}
	data := mutation.Serialize()

	decoded, _, err := DeserializeMutation(data)
	require.NoError(t, err)

	for i := range data {
		data[i] = 0xff
	}
	require.Equal(t, mutation, *decoded)
}

func TestDeserializeMutationRejectsTruncatedData(t *testing.T) {
	mutation := Mutation{Key: []byte("key"), PreviousValue: []byte("old"), NewValue: []byte("new")}
	data := mutation.Serialize()

	for length := 0; length < len(data); length++ {
		_, _, err := DeserializeMutation(data[:length])
		require.Error(t, err, "truncated to %d bytes", length)
	}
}

func TestDeserializeMutationRejectsInvalidData(t *testing.T) {
	valid := (&Mutation{Key: []byte("k"), PreviousValue: []byte("pv"), NewValue: []byte("nv")}).Serialize()

	// Byte offsets into valid; see TestMutationSerializedLayout.
	const (
		versionOffset              = 0
		previousValueUnknownOffset = 6
		previousValuePresentOffset = 7
		newValuePresentOffset      = 14
	)

	tests := []struct {
		name   string
		mutate func(data []byte)
	}{
		{
			name:   "unsupported version",
			mutate: func(data []byte) { data[versionOffset] = 1 },
		},
		{
			name:   "invalid previous value unknown byte",
			mutate: func(data []byte) { data[previousValueUnknownOffset] = 2 },
		},
		{
			name:   "invalid previous value present byte",
			mutate: func(data []byte) { data[previousValuePresentOffset] = 2 },
		},
		{
			name:   "invalid new value present byte",
			mutate: func(data []byte) { data[newValuePresentOffset] = 2 },
		},
		{
			name:   "previous value not present but has length",
			mutate: func(data []byte) { data[previousValuePresentOffset] = 0 },
		},
		{
			name:   "new value not present but has length",
			mutate: func(data []byte) { data[newValuePresentOffset] = 0 },
		},
		{
			name:   "key length past end of data",
			mutate: func(data []byte) { data[1] = 0xff },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := bytes.Clone(valid)
			tt.mutate(data)
			_, _, err := DeserializeMutation(data)
			require.Error(t, err)
		})
	}
}

func TestMutationsRoundTrip(t *testing.T) {
	tests := []struct {
		name      string
		mutations []Mutation
	}{
		{name: "empty", mutations: []Mutation{}},
		{name: "one", mutations: sampleMutations()[:1]},
		{name: "several", mutations: sampleMutations()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decoded, err := DeserializeMutations(SerializeMutations(tt.mutations))
			require.NoError(t, err)
			require.Equal(t, tt.mutations, decoded)
		})
	}
}

func TestSerializeMutationsConcatenatesMutations(t *testing.T) {
	mutations := sampleMutations()
	var expected []byte
	for i := range mutations {
		expected = append(expected, mutations[i].Serialize()...)
	}
	require.Equal(t, expected, SerializeMutations(mutations))
}

func TestDeserializeMutationsRejectsTruncatedFinalMutation(t *testing.T) {
	data := SerializeMutations(sampleMutations())
	_, err := DeserializeMutations(data[:len(data)-1])
	require.Error(t, err)
}

func TestDeserializeMutationsRejectsTrailingData(t *testing.T) {
	data := append(SerializeMutations(sampleMutations()), 0)
	_, err := DeserializeMutations(data)
	require.Error(t, err)
}
