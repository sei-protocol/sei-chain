//go:build mock_chain_validation

package types

// ConsensusPolicy in mock_chain_validation builds swallows every
// swallow-eligible kind enumerated by ValidationErrorKinds — the chain
// computes every check authentically, logs nothing here, but does not
// halt on failure (counter is incremented in HandleError).
type ConsensusPolicy struct{}

var swallowedKinds = func() map[ErrorKind]struct{} {
	m := make(map[ErrorKind]struct{}, len(ValidationErrorKinds()))
	for _, k := range ValidationErrorKinds() {
		m[k] = struct{}{}
	}
	return m
}()

func (ConsensusPolicy) HandleError(kind ErrorKind, err error) error {
	if _, ok := swallowedKinds[kind]; ok {
		recordUnsafeValidationSkipped(kind)
		return nil
	}
	return err
}
