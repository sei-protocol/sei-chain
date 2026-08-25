package registry

import "fmt"

// envCannotDeliver holds the keys an environment variable cannot supply, with the reason.
var envCannotDeliver = map[string]string{}

// refusedBy records which section recorded each refusal, so one covering nothing can name it.
var refusedBy = map[string]string{}

// RefuseFromEnvironment records that an environment variable cannot supply a key.
//
// An environment carries one string per name. Most readers cast that string into whatever the setting
// needs, so the environment works for them. A reader that takes its value's exact type instead cannot be
// handed a string at all, and no spelling of the variable would satisfy it.
//
// Resolving such a key from the environment puts an unusable value at the top of the order, and installing
// it stops the node. Leaving the channel out means the file's value applies and the node runs. That is
// deliberately not what the machinery this replaces does, which resolves the variable and refuses to
// start, so the difference is recorded rather than assumed. A value silently doing nothing is the failure
// this whole surface exists to remove, which is why the reason is required and not optional.
//
// section is the section that declares the key, and it is kept so a refusal that covers nothing can name
// the registration that recorded it. That is the case where attribution is worth anything: the key is
// misspelled, no section declares it, and what an author needs to know is which registration to look at.
// Whether the key is one that section declares is answered when something resolves, because a refusal may
// be recorded before the registration it belongs to.
//
// Called from the owning package, beside its registration, so the reason sits with the code that knows it.
func RefuseFromEnvironment(section, key, reason string) {
	mu.Lock()
	defer mu.Unlock()
	if reason == "" {
		defects = append(defects, Defect{Section: section, Err: fmt.Errorf(
			"refusing %q from the environment with no reason; an operator whose variable is ignored has "+
				"to be told why", key)})
		return
	}
	envCannotDeliver[key] = reason
	refusedBy[key] = section
}

// RefusedBy returns the section that recorded a key's refusal, and whether one did.
func RefusedBy(key string) (string, bool) {
	mu.RLock()
	defer mu.RUnlock()
	section, ok := refusedBy[key]
	return section, ok
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
