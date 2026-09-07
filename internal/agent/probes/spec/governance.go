package spec

// GovernanceFields is the schema of the governance block an operator may
// put on any probe instance, and that snmp_poll also accepts per discovery
// rule. There is one definition so every form and every check reads the
// same shape as the governance package parses.
func GovernanceFields() []ParamSpec {
	return []ParamSpec{
		{Key: "owner", Kind: KindBlock, Description: "Who owns what this instance observes", Fields: []ParamSpec{
			{Key: "team", Kind: KindString, Description: "Owning team"},
			{Key: "contact", Kind: KindString, Description: "Contact for the team"},
		}},
		{Key: "criticality", Kind: KindString, Enum: []string{"critical", "high", "medium", "low"}, Description: "Business criticality"},
		{Key: "location", Kind: KindBlock, Description: "Where it is", Fields: []ParamSpec{
			{Key: "site", Kind: KindString},
			{Key: "datacenter", Kind: KindString},
			{Key: "rack", Kind: KindString},
			{Key: "room", Kind: KindString},
		}},
		{Key: "lifecycle", Kind: KindString, Description: "active, maintenance, decommissioning or retired"},
		{Key: "labels", Kind: KindMap, Description: "Free-form labels, emitted as entity.label.<key>; use application to name the application chain"},
	}
}

// CheckGovernance reports the governance keys that are unknown or of the
// wrong shape. Values are left to the governance package, which owns the
// closed sets.
func CheckGovernance(v interface{}) []SpecProblem {
	if v == nil {
		return nil
	}
	m, ok := asStringMap(v)
	if !ok {
		return []SpecProblem{{Key: "governance", Kind: ProblemShape, Message: "governance must be a mapping"}}
	}
	var out []SpecProblem
	checkBlock("governance.", GovernanceFields(), m, &out)
	for i := range out {
		if out[i].Kind == ProblemUnknown {
			out[i].Message = "not a governance key"
		}
	}
	return out
}
