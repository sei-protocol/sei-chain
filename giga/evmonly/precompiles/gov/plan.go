package gov

// Plan is a scheduled software upgrade: at Height, a binary that does not know
// Name must stop before executing the block.
type Plan struct {
	Name   string
	Height uint64
	Info   string
	// Proposal is the ID of the proposal that scheduled the plan.
	Proposal uint64
}

// ReadPlan returns the scheduled upgrade plan in db, if there is one.
func ReadPlan(db Reader) (Plan, bool) {
	return store{addr: Address, db: db}.plan()
}

// ReadDoneHeight returns the height at which the upgrade named name was
// completed in db, if it was.
func ReadDoneHeight(db Reader, name string) (uint64, bool) {
	height := store{addr: Address, db: db}.u64(doneSlot(name))
	return height, height != 0
}

func (s store) plan() (Plan, bool) {
	height := s.u64(planSlot(planFieldHeight))
	if height == 0 {
		return Plan{}, false
	}
	return Plan{
		Name:     s.str(planSlot(planFieldName)),
		Height:   height,
		Info:     s.str(planSlot(planFieldInfo)),
		Proposal: s.u64(planSlot(planFieldProposal)),
	}, true
}

func (w writer) setPlan(plan Plan, proposal uint64) {
	w.setU64(planSlot(planFieldHeight), plan.Height)
	w.setStr(planSlot(planFieldName), plan.Name)
	w.setStr(planSlot(planFieldInfo), plan.Info)
	w.setU64(planSlot(planFieldProposal), proposal)
}

func (w writer) clearPlan() {
	w.setPlan(Plan{}, 0)
}

// completeDuePlan clears a plan due at or before height and records its name
// as done at height.
func (w writer) completeDuePlan(height uint64) {
	plan, ok := w.plan()
	if !ok || plan.Height > height {
		return
	}
	w.clearPlan()
	w.setU64(doneSlot(plan.Name), height)
}
