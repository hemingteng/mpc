//
// Copyright (c) 2020 Markku Rossi
//
// All rights reserved.
//

package ssa

import (
	"fmt"
	"io"
	"strings"

	"github.com/markkurossi/mpc/compiler/types"
)

// Block implements a basic block.
type Block struct {
	ID         string
	Name       string
	From       []*Block
	Next       *Block
	BranchCond Variable
	Branch     *Block
	Instr      []Instr
	Bindings   Bindings
	Dead       bool
	Processed  bool
}

func (b *Block) String() string {
	return b.ID
}

// Equals tests if the argument block is equal to this basic block.
func (b *Block) Equals(o *Block) bool {
	return b.ID == o.ID
}

// SetNext sets the next basic block.
func (b *Block) SetNext(o *Block) {
	if b.Next != nil && b.Next.ID != o.ID {
		panic(fmt.Sprintf("%s.Next already set to %s, now setting to %s",
			b.ID, b.Next.ID, o.ID))
	}
	b.Next = o
	o.addFrom(b)
}

// SetBranch sets the argument block being a branch block for this
// basic block.
func (b *Block) SetBranch(o *Block) {
	if b.Branch != nil && b.Branch.ID != o.ID {
		panic(fmt.Sprintf("%s.Branch already set to %s, now setting to %s",
			b.ID, b.Next.ID, o.ID))
	}
	b.Branch = o
	o.addFrom(b)
}

func (b *Block) addFrom(o *Block) {
	for _, f := range b.From {
		if f.Equals(o) {
			return
		}
	}
	b.From = append(b.From, o)
}

// AddInstr adds an instruction to this basic block.
func (b *Block) AddInstr(instr Instr) {
	b.Instr = append(b.Instr, instr)
}

// ReturnBinding returns the return statement binding for the argument
// variable. If the block contains a branch and variable's value is
// modified in both branches, the function adds a Phi instruction to
// resolve the variable's value after this basic block.
func (b *Block) ReturnBinding(name string, retBlock *Block, gen *Generator) (
	v Variable, ok bool) {

	// XXX Check if the if-ssagen could omit branch in this case?
	if b.Branch == nil || b.Next == b.Branch {
		// Sequential block, return latest value
		if b.Next != nil {
			v, ok = b.Next.ReturnBinding(name, retBlock, gen)
			if ok {
				return v, true
			}
			// Next didn't have value, take ours below.
		}
		bind, ok := b.Bindings.Get(name)
		if !ok {
			return v, false
		}
		return bind.Value(retBlock, gen), true
	}
	vTrue, ok := b.Branch.ReturnBinding(name, retBlock, gen)
	if !ok {
		return v, false
	}
	vFalse, ok := b.Next.ReturnBinding(name, retBlock, gen)
	if !ok {
		return v, false
	}
	if vTrue.Equal(&vFalse) {
		return vTrue, true
	}

	var rType types.Info
	if vTrue.Type.Bits > vFalse.Type.Bits {
		rType = vTrue.Type
	} else {
		rType = vFalse.Type
	}

	v = gen.AnonVar(rType)
	retBlock.AddInstr(NewPhiInstr(b.BranchCond, vTrue, vFalse, v))

	return v, true
}

// Serialize serializes the basic block's instructions.
func (b *Block) Serialize() []Step {
	seen := make(map[string]bool)
	return b.serialize(nil, seen)
}

func (b *Block) serialize(code []Step, seen map[string]bool) []Step {
	if seen[b.ID] {
		return code
	}
	// Have all predecessors been processed?
	for _, from := range b.From {
		if !seen[from.ID] {
			return code
		}
	}
	seen[b.ID] = true

	var label string
	if len(b.Name) > 0 {
		label = b.Name
	}

	for _, instr := range b.Instr {
		code = append(code, Step{
			Label: label,
			Instr: instr,
		})
		label = ""
	}

	if b.Branch != nil {
		code = b.Branch.serialize(code, seen)
	}
	if b.Next != nil {
		code = b.Next.serialize(code, seen)
	}
	return code
}

// DotNodes creates graphviz dot description of this basic block.
func (b *Block) DotNodes(out io.Writer, seen map[string]bool) {
	if seen[b.ID] {
		return
	}
	seen[b.ID] = true

	var label string
	if len(b.Instr) == 1 {
		label = b.Instr[0].string(0, false)
	} else {
		var maxLen int
		for _, i := range b.Instr {
			l := len(i.Op.String())
			if l > maxLen {
				maxLen = l
			}
		}
		for _, i := range b.Instr {
			label += i.string(maxLen, false)
			label += "\\l"
		}
	}

	fmt.Fprintf(out, "  %s [label=\"%s\"]\n", b.ID,
		strings.ReplaceAll(label, `"`, `\"`))

	if b.Next != nil {
		b.Next.DotNodes(out, seen)
	}
	if b.Branch != nil {
		b.Branch.DotNodes(out, seen)
	}
}

// DotLinks creates graphviz dot description of the links to and from
// this basic block.
func (b *Block) DotLinks(out io.Writer, seen map[string]bool) {
	if seen[b.ID] {
		return
	}
	seen[b.ID] = true
	if b.Next != nil {
		fmt.Fprintf(out, "  %s -> %s [label=\"%s\"];\n",
			b.ID, b.Next.ID, b.Next.ID)
	}
	if b.Branch != nil {
		fmt.Fprintf(out, "  %s -> %s [label=\"%s\"];\n",
			b.ID, b.Branch.ID, b.Branch.ID)
	}

	if b.Next != nil {
		b.Next.DotLinks(out, seen)
	}
	if b.Branch != nil {
		b.Branch.DotLinks(out, seen)
	}
}

// Dot creates a graphviz dot description of this basic block.
func Dot(out io.Writer, block *Block) {
	fontname := "Courier"
	fontsize := 10

	fmt.Fprintln(out, "digraph program {")
	fmt.Fprintf(out, "  node [shape=box fontname=\"%s\" fontsize=\"%d\"]\n",
		fontname, fontsize)
	fmt.Fprintf(out, "  edge [fontname=\"%s\" fontsize=\"%d\"]\n",
		fontname, fontsize)
	block.DotNodes(out, make(map[string]bool))
	block.DotLinks(out, make(map[string]bool))
	fmt.Fprintln(out, "}")
}
