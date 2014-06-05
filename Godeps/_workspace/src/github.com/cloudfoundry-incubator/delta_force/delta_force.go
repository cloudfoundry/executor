package delta_force

type ActualInstance struct {
	Index int
	Guid  string
}

type Result struct {
	IndicesToStart       []int
	GuidsToStop          []string
	IndicesToStopOneGuid []int
}

func (r Result) Empty() bool {
	return len(r.IndicesToStart) == 0 && len(r.GuidsToStop) == 0 && len(r.IndicesToStopOneGuid) == 0
}

type ActualInstances []ActualInstance

func (a ActualInstances) hasIndex(index int) bool {
	for _, actual := range a {
		if actual.Index == index {
			return true
		}
	}

	return false
}

func (a ActualInstances) numAtIndex(index int) int {
	count := 0
	for _, actual := range a {
		if actual.Index == index {
			count++
		}
	}

	return count
}

func Reconcile(numDesired int, actuals ActualInstances) Result {
	result := Result{}

	for i := 0; i < numDesired; i++ {
		if !actuals.hasIndex(i) {
			result.IndicesToStart = append(result.IndicesToStart, i)
		}
	}

	if len(result.IndicesToStart) > 0 {
		return result
	}

	for _, actual := range actuals {
		if actual.Index >= numDesired {
			result.GuidsToStop = append(result.GuidsToStop, actual.Guid)
		}
	}

	for i := 0; i < numDesired; i++ {
		if actuals.numAtIndex(i) > 1 {
			result.IndicesToStopOneGuid = append(result.IndicesToStopOneGuid, i)
		}
	}

	return result
}
