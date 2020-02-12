//
// compiler.go
//
// Copyright (c) 2019 Markku Rossi
//
// All rights reserved.
//

package compiler

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/markkurossi/mpc/circuit"
)

func Compile(data string) (*circuit.Circuit, error) {
	return compile("{data}", strings.NewReader(data))
}

func CompileFile(file string) (*circuit.Circuit, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	return compile(file, f)
}

func compileCircuit(name string, in io.Reader) (*circuit.Circuit, error) {
	parser := NewParser(name, in)
	_, err := parser.Parse()
	if err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("not implemented yet")
}

func compile(name string, in io.Reader) (*circuit.Circuit, error) {
	parser := NewParser(name, in)
	unit, err := parser.Parse()
	if err != nil {
		return nil, err
	}
	return unit.Compile()
}