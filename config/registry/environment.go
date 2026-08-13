package registry

import (
	"fmt"
	"sort"
)

// envCannotDeliver holds the keys an environment variable cannot supply, with the reason.
var envCannotDeliver = map[string]string{}

// RefuseFromEnvironment records that an environment variable cannot supply a key.
//
// An environment carries one string per name. Most readers cast that into whatever the setting needs, so
// the environment works for them. A reader that takes its value's exact type instead cannot be handed a
// string at all, and there is no spelling of the variable that would satisfy it.
//
// Resolving such a key from the environment would put an unusable value at the top of the order and install
// it, which stops the node. Skipping the channel means the file's value applies instead, so the node runs.
// That is deliberately not what the machinery this replaces does: it resolves the variable and the node
// refuses to start. The difference is recorded rather than assumed, and a diagnostic reports the variable as
// ignored, because a value silently doing nothing is the failure this whole surface exists to remove.
//
// Called from the owning package, beside its registration, so the reason sits with the code that knows it.
func RefuseFromEnvironment(key, reason string) {
	mu.Lock()
	defer mu.Unlock()
	if reason == "" {
		defects = append(defects, Defect{Section: key, Err: fmt.Errorf(
			"refusing %q from the environment with no reason; an operator told their variable is ignored "+
				"needs to know why", key)})
		return
	}
	envCannotDeliver[key] = reason
}

// EnvCannotDeliver returns the keys an environment variable cannot supply, and why.
func EnvCannotDeliver() map[string]string {
	mu.RLock()
	defer mu.RUnlock()
	out := make(map[string]string, len(envCannotDeliver))
	for key, reason := range envCannotDeliver {
		out[key] = reason
	}
	return out
}

// EnvRefusedKeys returns those keys, sorted.
func EnvRefusedKeys() []string {
	refused := EnvCannotDeliver()
	out := make([]string, 0, len(refused))
	for key := range refused {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
