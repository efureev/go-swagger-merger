package yamlx

import (
	"errors"
	"fmt"

	"gopkg.in/yaml.v3"
)

// ErrAliasBudget is returned when alias expansion would produce more nodes
// than the caller allowed.
var ErrAliasBudget = errors.New("yaml alias expansion exceeded budget")

// ErrAliasCycle is returned when anchors reference each other in a loop.
var ErrAliasCycle = errors.New("yaml alias cycle")

// DefaultAliasBudget bounds the node count produced by expanding anchors.
const DefaultAliasBudget = 1 << 20

const maxAliasDepth = 256

// ExpandAliases rewrites alias nodes into copies of their anchor targets and
// applies YAML merge keys ("<<"), leaving a self-contained tree.
//
// Two reasons this is mandatory rather than cosmetic. An alias node carries a
// pointer into its own document, so moving it into the merged document leaves
// a reference dangling. And expansion is where an untrusted input can explode:
// budget caps the total nodes produced, which is what stops a billion-laughs
// document.
func ExpandAliases(n *yaml.Node, budget int) error {
	if budget <= 0 {
		budget = DefaultAliasBudget
	}
	e := &expander{
		budget:  budget,
		done:    map[*yaml.Node]*yaml.Node{},
		active:  map[*yaml.Node]bool{},
		visited: map[*yaml.Node]bool{},
	}
	_, err := e.expand(n, 0)
	return err
}

type expander struct {
	budget  int
	spent   int
	done    map[*yaml.Node]*yaml.Node
	active  map[*yaml.Node]bool
	visited map[*yaml.Node]bool
}

func (e *expander) charge(n int) error {
	e.spent += n
	if e.spent > e.budget {
		return fmt.Errorf("%w: %d nodes", ErrAliasBudget, e.spent)
	}
	return nil
}

// expand returns the node that should take n's place.
func (e *expander) expand(n *yaml.Node, depth int) (*yaml.Node, error) {
	if n == nil {
		return nil, nil
	}
	if depth > maxAliasDepth {
		return nil, fmt.Errorf("%w: nesting deeper than %d", ErrAliasBudget, maxAliasDepth)
	}

	if n.Kind == yaml.AliasNode {
		target := n.Alias
		if target == nil {
			return nil, fmt.Errorf("%w: alias %q resolves to nothing", ErrAliasCycle, n.Value)
		}
		if e.active[target] {
			return nil, fmt.Errorf("%w: anchor %q", ErrAliasCycle, n.Value)
		}
		resolved, err := e.expandTarget(target, depth)
		if err != nil {
			return nil, err
		}
		if err := e.charge(countNodes(resolved)); err != nil {
			return nil, err
		}
		cp := Clone(resolved)
		clearAnchors(cp)
		// Comments written at the alias site outrank the anchor's.
		if n.HeadComment != "" {
			cp.HeadComment = n.HeadComment
		}
		if n.LineComment != "" {
			cp.LineComment = n.LineComment
		}
		return cp, nil
	}

	if e.visited[n] {
		return n, nil
	}
	e.visited[n] = true
	e.active[n] = true
	defer delete(e.active, n)

	for i, c := range n.Content {
		nc, err := e.expand(c, depth+1)
		if err != nil {
			return nil, err
		}
		n.Content[i] = nc
	}
	n.Anchor = ""
	if n.Kind == yaml.MappingNode {
		applyMergeKeys(n)
	}
	return n, nil
}

// expandTarget expands an anchor target once and memoises it, so that N
// aliases onto the same anchor cost one traversal rather than N.
func (e *expander) expandTarget(target *yaml.Node, depth int) (*yaml.Node, error) {
	if cached, ok := e.done[target]; ok {
		return cached, nil
	}
	resolved, err := e.expand(target, depth+1)
	if err != nil {
		return nil, err
	}
	e.done[target] = resolved
	return resolved, nil
}

// applyMergeKeys folds a "<<" entry into its mapping. Per YAML 1.1, keys
// already present win, and for a sequence of sources earlier entries win.
func applyMergeKeys(n *yaml.Node) {
	_, v, ok := MapGet(n, "<<")
	if !ok {
		return
	}
	MapDelete(n, "<<")

	var sources []*yaml.Node
	switch {
	case IsMapping(v):
		sources = []*yaml.Node{v}
	case IsSequence(v):
		sources = v.Content
	default:
		return
	}
	for _, src := range sources {
		if !IsMapping(src) {
			continue
		}
		for _, entry := range Entries(src) {
			if _, _, exists := MapGet(n, entry.Key); exists {
				continue
			}
			MapSetNode(n, Clone(entry.KeyN), Clone(entry.Value))
		}
	}
}

func clearAnchors(n *yaml.Node) {
	if n == nil {
		return
	}
	n.Anchor = ""
	for _, c := range n.Content {
		clearAnchors(c)
	}
}

func countNodes(n *yaml.Node) int {
	if n == nil {
		return 0
	}
	total := 1
	for _, c := range n.Content {
		total += countNodes(c)
	}
	return total
}
