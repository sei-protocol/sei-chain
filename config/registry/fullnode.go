package registry

// IsFullnodeMode reports whether a mode runs the services a fullnode serves.
//
// One definition, because more than one package needs it and each of them sits on a different side of
// an import edge. app/params owns the node mode a node is started with; a section's own package needs
// the same fact to state a baseline that varies on it, and cannot import app/params because params
// imports the section. Both read this instead of spelling out which modes qualify.
//
// Archive counts. It serves queries, which is the property this names, and it is the mode most easily
// forgotten when the rule is written out by hand.
func IsFullnodeMode(mode Mode) bool {
	return mode == ModeFull || mode == ModeArchive
}
