package engine

const (
	budgetSmallTotal  = 80
	budgetMediumTotal = 120
	budgetLargeTotal  = 160
	budgetHardCap     = 240
	depthBonusLines   = 20

	// Allocation percentages.
	allocDefinition = 0.50
	allocReferences = 0.25
	allocCallers    = 0.15
	allocMeta       = 0.10
)

// ComputeBudget returns a line budget based on the --budget flag and --depth.
func ComputeBudget(budgetFlag string, depth int) SymBudget {
	total := baseTotal(budgetFlag, depth)
	if depth > 1 {
		total += depthBonusLines * (depth - 1)
	}
	if total > budgetHardCap {
		total = budgetHardCap
	}

	return allocate(total)
}

func baseTotal(flag string, depth int) int {
	switch flag {
	case "small":
		return budgetSmallTotal
	case "medium":
		return budgetMediumTotal
	case "large":
		return budgetLargeTotal
	default: // "auto" or empty
		if depth > 1 {
			return budgetMediumTotal
		}
		return budgetSmallTotal
	}
}

func allocate(total int) SymBudget {
	def := int(float64(total) * allocDefinition)
	refs := int(float64(total) * allocReferences)
	callers := int(float64(total) * allocCallers)
	meta := total - def - refs - callers // remainder goes to meta
	return SymBudget{
		TotalLines:      total,
		DefinitionLines: def,
		ReferenceLines:  refs,
		CallerLines:     callers,
		MetaLines:       meta,
	}
}
