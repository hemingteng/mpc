//
// Copyright (c) 2020 Markku Rossi
//
// All rights reserved.
//

package ssa

import (
	"fmt"

	"github.com/markkurossi/mpc/compiler/types"
)

var (
	_ BindingValue = &Variable{}
	_ BindingValue = &Select{}
)

// Bindings defines variable bindings.
type Bindings []Binding

// Set adds a new binding for the variable.
func (bindings *Bindings) Set(v Variable, val *Variable) {
	for idx, b := range *bindings {
		if b.Name == v.Name && b.Scope == v.Scope {
			b.Type = v.Type
			if val != nil {
				b.Bound = val
			} else {
				b.Bound = &v
			}
			(*bindings)[idx] = b
			return
		}
	}

	b := Binding{
		Name:  v.Name,
		Scope: v.Scope,
		Type:  v.Type,
	}
	if val != nil {
		b.Bound = val
	} else {
		b.Bound = &v
	}

	*bindings = append(*bindings, b)
}

// Get gets the variable binding.
func (bindings Bindings) Get(name string) (ret Binding, ok bool) {
	for _, b := range bindings {
		if b.Name == name {
			if len(ret.Name) == 0 || b.Scope > ret.Scope {
				ret = b
			}
		}
	}
	if len(ret.Name) == 0 {
		return ret, false
	}
	return ret, true
}

// Clone makes a copy of the bindings.
func (bindings Bindings) Clone() Bindings {
	result := make(Bindings, len(bindings))
	copy(result, bindings)
	return result
}

// Merge merges the argument false-branch bindings into this bindings
// instance that represents the true-branch values.
func (bindings Bindings) Merge(cond Variable, falseBindings Bindings) Bindings {
	names := make(map[string]bool)

	for _, b := range bindings {
		names[b.Name] = true
	}
	for _, b := range falseBindings {
		names[b.Name] = true
	}

	var result Bindings
	for name := range names {
		bTrue, ok1 := bindings.Get(name)
		bFalse, ok2 := falseBindings.Get(name)
		if !ok1 && !ok2 {
			continue
		} else if !ok1 {
			result = append(result, bFalse)
		} else if !ok2 {
			result = append(result, bTrue)
		} else {
			if bTrue.Bound.Equal(bFalse.Bound) {
				result = append(result, bTrue)
			} else {
				var phiType types.Info
				if bTrue.Type.Bits > bFalse.Type.Bits {
					phiType = bTrue.Type
				} else {
					phiType = bFalse.Type
				}

				result = append(result, Binding{
					Name:  name,
					Scope: bTrue.Scope,
					Type:  phiType,
					Bound: &Select{
						Cond:  cond,
						Type:  phiType,
						True:  bTrue.Bound,
						False: bFalse.Bound,
					},
				})
			}
		}
	}
	return result
}

// Binding implements a variable binding.
type Binding struct {
	Name  string
	Scope int
	Type  types.Info
	Bound BindingValue
}

func (b Binding) String() string {
	return fmt.Sprintf("%s@%d/%s=%s", b.Name, b.Scope, b.Type, b.Bound)
}

// Value returns the binding value.
func (b Binding) Value(block *Block, gen *Generator) Variable {
	return b.Bound.Value(block, gen)
}

// BindingValue represents variable binding.
type BindingValue interface {
	Equal(o BindingValue) bool
	Value(block *Block, gen *Generator) Variable
}

// Select implements Phi-bindings for variable.
type Select struct {
	Cond     Variable
	Type     types.Info
	True     BindingValue
	False    BindingValue
	Resolved Variable
}

func (phi *Select) String() string {
	return fmt.Sprintf("\u03D5(%s|%s|%s)/%s",
		phi.Cond, phi.True, phi.False, phi.Type)
}

// Equal tests if this select binding is equal to the argument bindind
// value.
func (phi *Select) Equal(other BindingValue) bool {
	o, ok := other.(*Select)
	if !ok {
		return false
	}
	if !phi.Cond.Equal(&o.Cond) {
		return false
	}
	return phi.True.Equal(o.True) && phi.False.Equal(o.False)
}

// Value returns the binding value.
func (phi *Select) Value(block *Block, gen *Generator) Variable {
	if phi.Resolved.Type.Type != types.Undefined {
		return phi.Resolved
	}

	t := phi.True.Value(block, gen)
	f := phi.False.Value(block, gen)
	v := gen.AnonVar(phi.Type)
	block.AddInstr(NewPhiInstr(phi.Cond, t, f, v))

	phi.Resolved = v

	return v
}
