package atlantis

import (
	"fmt"
	"testing"
)

// fuzzGraph builds a dependency graph of up to 16 nodes from raw bytes: each
// pair of bytes (a, b) adds the edge "node a depends on node b".
func fuzzGraph(raw []byte) map[string]map[string]struct{} {
	const nodes = 16
	name := func(b byte) string { return fmt.Sprintf("n%02d", int(b)%nodes) }
	deps := map[string]map[string]struct{}{}
	for i := 0; i < nodes; i++ {
		deps[name(byte(i))] = map[string]struct{}{}
	}
	for i := 0; i+1 < len(raw); i += 2 {
		from, to := name(raw[i]), name(raw[i+1])
		if from != to {
			deps[from][to] = struct{}{}
		}
	}
	return deps
}

// reachable reports whether to can be reached from from.
func reachable(deps map[string]map[string]struct{}, from, to string) bool {
	seen := map[string]bool{}
	stack := []string{from}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == to {
			return true
		}
		if seen[n] {
			continue
		}
		seen[n] = true
		for d := range deps[n] {
			stack = append(stack, d)
		}
	}
	return false
}

// FuzzExecutionOrderGroups checks that every dependency lands in a strictly
// lower execution_order_group, unless the two stacks are on a common cycle,
// and that only stacks on a cycle are reported as cyclic.
func FuzzExecutionOrderGroups(f *testing.F) {
	f.Add([]byte{0, 1, 1, 2})
	f.Add([]byte{0, 1, 1, 0})
	// d depends on a cycle (a<->b) and on a chain n2 -> n1.
	f.Add([]byte{0, 1, 1, 0, 4, 0, 4, 3, 3, 2})
	f.Add([]byte{0, 1, 1, 2, 2, 0, 3, 2, 5, 3, 5, 4})

	f.Fuzz(func(t *testing.T, raw []byte) {
		deps := fuzzGraph(raw)
		groups, cyclic := computeExecutionOrderGroups(deps)

		inCycle := map[string]bool{}
		for _, dir := range cyclic {
			inCycle[dir] = true
		}
		for dir := range deps {
			onCycle := false
			for dep := range deps[dir] {
				if reachable(deps, dep, dir) {
					onCycle = true
				}
			}
			if onCycle != inCycle[dir] {
				t.Fatalf("%s: on a cycle = %v, reported cyclic = %v (graph %v)", dir, onCycle, inCycle[dir], deps)
			}
			for dep := range deps[dir] {
				if reachable(deps, dep, dir) {
					continue // same cycle: ordering can't be honored
				}
				if groups[dep] >= groups[dir] {
					t.Fatalf("%s (group %d) depends on %s (group %d), want a lower group (graph %v)",
						dir, groups[dir], dep, groups[dep], deps)
				}
			}
		}
	})
}
