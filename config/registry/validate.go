package registry

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/mitchellh/mapstructure"
)

// Validator is what a section implements to state rules its own keys have to satisfy.
//
// The tags say what shape a value has and this says what values are allowed. An enum's member set and
// a number's range live here, because they are facts about the setting rather than about its type, and
// the section is the only place that knows them.
type Validator interface {
	Validate() error
}

// SectionError is one section reporting that its resolved configuration is not usable.
type SectionError struct {
	// Section is the section that refused.
	Section string
	// Err is what it said.
	Err error
}

func (e SectionError) Error() string { return e.Section + ": " + e.Err.Error() }

// ValidateResolved asks every section whether the values resolved for it are usable, sorted by section.
//
// Run over the resolved configuration rather than over what a file writes, because a rule can span two
// keys and a section handed only the written ones would see a zero for every key the operator left
// alone. What it judges is therefore what the node would run.
//
// A section declaring no rules is skipped rather than passed, so declaring a section costs nothing until
// it has something to say.
func ValidateResolved(resolved Resolved) []SectionError {
	mu.RLock()
	defer mu.RUnlock()

	var out []SectionError
	for name, section := range sections {
		if section.Validate == nil {
			continue
		}
		if err := section.Validate(valuesFor(name, resolved)); err != nil {
			out = append(out, SectionError{Section: name, Err: err})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Section < out[j].Section })
	return out
}

// valuesFor collects one section's resolved keys, with the section prefix removed.
func valuesFor(name string, resolved Resolved) map[string]any {
	prefix := name + "."
	out := map[string]any{}
	for key, resolution := range resolved.Keys {
		if strings.HasPrefix(key, prefix) {
			out[strings.TrimPrefix(key, prefix)] = resolution.Value
		}
	}
	return out
}

// validatorFor returns a function asking a section's own type whether its values are usable.
//
// Nil when the type states no rules, which is most of them. Detected from the type rather than declared
// separately at registration, so a section that grows a Validate method is checked from then on without
// anyone remembering to wire it.
//
// The struct is a validation target here and never a transport. It silently drops a key it does not
// model, which is why a node never reads through one, and that blind spot costs nothing here because an
// undeclared written key is reported by a different check.
func validatorFor(proto any) func(map[string]any) error {
	if proto == nil {
		return nil
	}
	protoType := reflect.TypeOf(proto)
	if !protoType.Implements(reflect.TypeOf((*Validator)(nil)).Elem()) {
		return nil
	}
	elem := protoType
	for elem.Kind() == reflect.Ptr {
		elem = elem.Elem()
	}

	return func(values map[string]any) error {
		instance := reflect.New(elem)
		if err := decodeInto(instance.Interface(), values); err != nil {
			return err
		}
		validator, ok := instance.Interface().(Validator)
		if !ok {
			return fmt.Errorf("this section's type states rules that cannot be run against it")
		}
		return validator.Validate()
	}
}

// decodeInto populates a section's struct from its resolved values.
//
// The same mapstructure tags the keys derive from, so a value reaches the field its key names and the
// two directions cannot disagree about which field a key belongs to.
func decodeInto(target any, values map[string]any) error {
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		Result:           target,
		WeaklyTypedInput: true,
		Metadata:         nil,
	})
	if err != nil {
		return fmt.Errorf("cannot read this section's values: %w", err)
	}
	if err := decoder.Decode(nest(values)); err != nil {
		return fmt.Errorf("cannot read this section's values: %w", err)
	}
	return nil
}

// nest turns dotted keys back into the shape the struct has.
//
// Keys arrive flat because that is how a configuration source carries them, and a struct with a nested
// field needs them nested again. The inverse of the walk that derived them.
func nest(values map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range values {
		segments := strings.Split(key, ".")
		level := out
		for _, segment := range segments[:len(segments)-1] {
			next, ok := level[segment].(map[string]any)
			if !ok {
				next = map[string]any{}
				level[segment] = next
			}
			level = next
		}
		level[segments[len(segments)-1]] = value
	}
	return out
}
