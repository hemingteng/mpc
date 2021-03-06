//
// Copyright (c) 2019 Markku Rossi
//
// All rights reserved.
//

package compiler

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/markkurossi/mpc/compiler/ast"
	"github.com/markkurossi/mpc/compiler/utils"
)

// Parser implements MPCL parser.
type Parser struct {
	compiler *Compiler
	logger   *utils.Logger
	lexer    *Lexer
	pkg      *ast.Package
}

// NewParser creates a new parser.
func NewParser(source string, compiler *Compiler, logger *utils.Logger,
	in io.Reader) *Parser {
	return &Parser{
		compiler: compiler,
		logger:   logger,
		lexer:    NewLexer(source, in),
	}
}

// Parse parses a package.
func (p *Parser) Parse(pkg *ast.Package) (*ast.Package, error) {
	name, err := p.parsePackage()
	if err != nil {
		return nil, err
	}
	if pkg == nil {
		p.pkg = ast.NewPackage(name)
	} else {
		// This source file must be in the same package.
		if name != pkg.Name {
			return nil, fmt.Errorf("found packages %s and %s", name, pkg.Name)
		}
		p.pkg = pkg
	}

	token, err := p.lexer.Get()
	if err != nil {
		if err != io.EOF {
			return nil, err
		}
		return p.pkg, nil
	}
	if token.Type == TSymImport {
		imports := make(map[string]string)
		_, err = p.needToken(TLParen)
		if err != nil {
			return nil, err
		}
		for {
			t, err := p.lexer.Get()
			if err != nil {
				return nil, err
			}
			if t.Type == TRParen {
				break
			}

			var alias string
			if t.Type == TIdentifier {
				alias = t.StrVal
				t, err = p.lexer.Get()
				if err != nil {
					return nil, err
				}
			}
			if t.Type != TConstant {
				return nil, p.errUnexpected(t, TConstant)
			}
			str, ok := t.ConstVal.(string)
			if !ok {
				return nil, p.errUnexpected(t, TConstant)
			}
			_, ok = imports[str]
			if ok {
				return nil, p.errf(t.From,
					"package %s imported more than once", str)
			}

			if len(alias) == 0 {
				parts := strings.Split(str, "/")
				alias = parts[len(parts)-1]
			}

			imports[alias] = str
		}
		p.pkg.Imports = imports
	} else {
		p.lexer.Unget(token)
	}

	for {
		err = p.parseToplevel()
		if err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}
	}

	return p.pkg, nil
}

func (p *Parser) errf(loc utils.Point, format string, a ...interface{}) error {
	msg := fmt.Sprintf(format, a...)

	p.lexer.FlushEOL()

	line, ok := p.lexer.history[loc.Line]
	if ok {
		var indicator []rune
		for i := 0; i < loc.Col; i++ {
			var r rune
			if line[i] == '\t' {
				r = '\t'
			} else {
				r = ' '
			}
			indicator = append(indicator, r)
		}
		indicator = append(indicator, '^')
		p.logger.Errorf(loc, "%s\n%s\n%s\n",
			msg, string(line), string(indicator))

		return errors.New(msg)
	}
	p.logger.Errorf(loc, "%s", msg)
	return errors.New(msg)
}

func (p *Parser) errUnexpected(offending *Token, expected TokenType) error {
	return p.errf(offending.From, "unexpected token '%s': expected '%s'",
		offending, expected)
}

func (p *Parser) needToken(tt TokenType) (*Token, error) {
	token, err := p.lexer.Get()
	if err != nil {
		return nil, err
	}
	if token.Type != tt {
		p.lexer.Unget(token)
		return nil, p.errUnexpected(token, tt)
	}
	return token, nil
}

func (p *Parser) sameLine(current utils.Point) bool {
	t, err := p.lexer.Get()
	if err != nil {
		return false
	}
	p.lexer.Unget(t)
	return t.From.Line == current.Line
}

func (p *Parser) parsePackage() (string, error) {
	t, err := p.needToken(TSymPackage)
	if err != nil {
		return "", err
	}
	t, err = p.needToken(TIdentifier)
	if err != nil {
		return "", err
	}
	parts := strings.Split(t.StrVal, "/")
	return parts[len(parts)-1], nil
}

func (p *Parser) parseToplevel() error {
	token, err := p.lexer.Get()
	if err != nil {
		return err
	}
	switch token.Type {
	case TSymConst:
		return p.parseConst()

	case TSymType:
		return p.parseTypeDecl()

	case TSymFunc:
		f, err := p.parseFunc(p.lexer.Annotations(token.From))
		if err != nil {
			return err
		}
		_, ok := p.pkg.Functions[f.Name]
		if ok {
			return p.errf(f.Loc, "function %s already defined", f.Name)
		}
		p.pkg.Functions[f.Name] = f

	default:
		return p.errf(token.From, "unexpected token '%s'", token.Type)
	}

	return nil
}

func (p *Parser) parseConst() error {
	token, err := p.lexer.Get()
	if err != nil {
		return err
	}
	switch token.Type {
	case TIdentifier:
		return p.parseConstDef(token)

	case TLParen:
		for {
			t, err := p.lexer.Get()
			if err != nil {
				return err
			}
			if t.Type == TRParen {
				return nil
			}
			err = p.parseConstDef(t)
			if err != nil {
				return err
			}
		}

	default:
		return p.errf(token.From, "unexpected token '%s'", token.Type)
	}
}

func (p *Parser) parseConstDef(token *Token) error {
	if token.Type != TIdentifier {
		return p.errf(token.From, "unexpected token '%s'", token.Type)
	}

	t, err := p.lexer.Get()
	if err != nil {
		return err
	}
	var constType *ast.TypeInfo
	if t.Type != TAssign {
		p.lexer.Unget(t)
		constType, err = p.parseType()
		if err != nil {
			return err
		}
		_, err = p.needToken(TAssign)
		if err != nil {
			return nil
		}
	}
	value, err := p.parseExprPrimary()
	if err != nil {
		return err
	}

	p.pkg.Constants = append(p.pkg.Constants, &ast.ConstantDef{
		Loc:  token.From,
		Name: token.StrVal,
		Type: constType,
		Init: value,
	})

	return nil
}

func (p *Parser) parseTypeDecl() error {
	name, err := p.needToken(TIdentifier)
	if err != nil {
		return err
	}
	t, err := p.lexer.Get()
	if err != nil {
		return err
	}
	switch t.Type {
	case TSymStruct:
		_, err := p.needToken(TLBrace)
		if err != nil {
		}
		var fields []ast.StructField
		for {
			t, err := p.lexer.Get()
			if err != nil {
				return err
			}
			if t.Type == TRBrace {
				break
			}
			var names []string
			for {
				if t.Type != TIdentifier {
					return p.errf(t.From, "unexpected token '%s'", t.Type)
				}
				names = append(names, t.StrVal)
				t, err = p.lexer.Get()
				if err != nil {
					return err
				}
				if t.Type != TComma {
					p.lexer.Unget(t)
					break
				}
				t, err = p.lexer.Get()
				if err != nil {
					return err
				}
			}

			typeInfo, err := p.parseType()
			if err != nil {
				return err
			}
			// Expand names.
			for _, n := range names {
				fields = append(fields, ast.StructField{
					Name: n,
					Type: typeInfo,
				})
			}
		}
		typeInfo := &ast.TypeInfo{
			Type:         ast.TypeStruct,
			TypeName:     name.StrVal,
			StructFields: fields,
		}
		p.pkg.Types = append(p.pkg.Types, typeInfo)
		return nil

	case TAssign:
		ti, err := p.parseType()
		if err != nil {
			return err
		}
		typeInfo := &ast.TypeInfo{
			Type:      ast.TypeAlias,
			TypeName:  name.StrVal,
			AliasType: ti,
		}
		p.pkg.Types = append(p.pkg.Types, typeInfo)
		return nil

	default:
		return p.errf(t.From, "unexpected token '%s'", t.Type)
	}
}

func (p *Parser) parseFunc(annotations ast.Annotations) (*ast.Func, error) {
	name, err := p.needToken(TIdentifier)
	if err != nil {
		return nil, err
	}

	_, err = p.needToken(TLParen)
	if err != nil {
		return nil, err
	}

	// Argument list.

	var arguments []*ast.Variable

	t, err := p.lexer.Get()
	if err != nil {
		return nil, err
	}
	if t.Type != TRParen {
		p.lexer.Unget(t)
		for {
			t, err = p.needToken(TIdentifier)
			if err != nil {
				return nil, err
			}
			arg := &ast.Variable{
				Loc:  t.From,
				Name: t.StrVal,
			}

			t, err = p.lexer.Get()
			if err != nil {
				return nil, err
			}
			if t.Type == TComma {
				arguments = append(arguments, arg)
				continue
			}
			p.lexer.Unget(t)

			// Type.
			typeInfo, err := p.parseType()
			if err != nil {
				return nil, err
			}
			arg.Type = typeInfo

			// All untyped arguments get this type.
			for i := len(arguments) - 1; i >= 0; i-- {
				if arguments[i].Type != nil {
					break
				}
				arguments[i].Type = typeInfo
			}

			// Append new argument.
			arguments = append(arguments, arg)

			t, err = p.lexer.Get()
			if err != nil {
				return nil, err
			}
			if t.Type == TRParen {
				break
			}
			if t.Type != TComma {
				return nil, p.errUnexpected(t, TComma)
			}
		}
	}

	// Return values.
	var returnValues []*ast.Variable

	n, err := p.lexer.Get()
	if err != nil {
		return nil, err
	}
	switch n.Type {
	case TLParen:
		for {
			typeInfo, err := p.parseType()
			if err != nil {
				return nil, err
			}
			returnValues = append(returnValues, &ast.Variable{
				Loc:  n.From,
				Type: typeInfo,
			})
			n, err = p.lexer.Get()
			if err != nil {
				return nil, err
			}
			if n.Type == TRParen {
				break
			}
			if n.Type != TComma {
				return nil, p.errUnexpected(n, TComma)
			}
		}
		_, err = p.needToken(TLBrace)
		if err != nil {
			return nil, err
		}

	case TLBrace:

	default:
		p.lexer.Unget(n)
		typeInfo, err := p.parseType()
		if err != nil {
			return nil, err
		}
		returnValues = append(returnValues, &ast.Variable{
			Loc:  n.From,
			Type: typeInfo,
		})
		_, err = p.needToken(TLBrace)
		if err != nil {
			return nil, err
		}
	}

	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}

	return ast.NewFunc(name.From, name.StrVal, arguments, returnValues, body,
		annotations), nil
}

func (p *Parser) parseBlock() (ast.List, error) {
	var result ast.List
	for {
		t, err := p.lexer.Get()
		if err != nil {
			return nil, err
		}
		if t.Type == TRBrace {
			break
		}
		p.lexer.Unget(t)

		ast, err := p.parseStatement()
		if err != nil {
			return nil, err
		}
		result = append(result, ast)
	}
	return result, nil
}

func (p *Parser) parseStatement() (ast.AST, error) {
	tStmt, err := p.lexer.Get()
	if err != nil {
		return nil, err
	}
	switch tStmt.Type {
	case TSymVar:
		var names []string
		for {
			tName, err := p.needToken(TIdentifier)
			if err != nil {
				return nil, err
			}
			names = append(names, tName.StrVal)
			t, err := p.lexer.Get()
			if err != nil {
				return nil, err
			}
			if t.Type != TComma {
				p.lexer.Unget(t)
				break
			}
		}
		typeInfo, err := p.parseType()
		if err != nil {
			return nil, err
		}

		t, err := p.lexer.Get()
		if err != nil {
			return nil, err
		}
		var expr ast.AST
		if t.Type == TAssign {
			// Initializer.
			expr, err = p.parseExpr()
			if err != nil {
				return nil, err
			}
		} else {
			p.lexer.Unget(t)
		}

		return &ast.VariableDef{
			Loc:   tStmt.From,
			Names: names,
			Type:  typeInfo,
			Init:  expr,
		}, nil

	case TSymIf:
		expr, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		_, err = p.needToken(TLBrace)
		if err != nil {
			return nil, err
		}

		var b1, b2 ast.List
		b1, err = p.parseBlock()
		if err != nil {
			return nil, err
		}
		t, err := p.lexer.Get()
		if err != nil {
			return nil, err
		}
		if t.Type == TSymElse {
			_, err = p.needToken(TLBrace)
			if err != nil {
				return nil, err
			}
			b2, err = p.parseBlock()
			if err != nil {
				return nil, err
			}
		} else {
			p.lexer.Unget(t)
		}
		return &ast.If{
			Expr:  expr,
			True:  b1,
			False: b2,
		}, nil

	case TSymReturn:
		var exprs []ast.AST
		if p.sameLine(tStmt.To) {
			expr, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			exprs = append(exprs, expr)
			for {
				t, err := p.lexer.Get()
				if err != nil {
					return nil, err
				}
				if t.Type != TComma {
					p.lexer.Unget(t)
					break
				}
				expr, err = p.parseExpr()
				if err != nil {
					return nil, err
				}
				exprs = append(exprs, expr)
			}
		}
		return &ast.Return{
			Loc:   tStmt.From,
			Exprs: exprs,
		}, nil

	case TSymFor:
		init, err := p.parseStatement()
		if err != nil {
			return nil, err
		}
		_, err = p.needToken(TSemicolon)
		if err != nil {
			return nil, err
		}
		cond, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		_, err = p.needToken(TSemicolon)
		if err != nil {
			return nil, err
		}
		inc, err := p.parseStatement()
		if err != nil {
			return nil, err
		}
		_, err = p.needToken(TLBrace)
		if err != nil {
			return nil, err
		}
		body, err := p.parseBlock()
		if err != nil {
			return nil, err
		}
		return &ast.For{
			Loc:  tStmt.From,
			Init: init,
			Cond: cond,
			Inc:  inc,
			Body: body,
		}, nil

	default:
		p.lexer.Unget(tStmt)
		lvalues, err := p.parseExprList()
		if err != nil {
			return nil, err
		}
		t, err := p.lexer.Get()
		if err != nil {
			return nil, err
		}
		switch t.Type {
		case TAssign, TDefAssign:
			values, err := p.parseExprList()
			if err != nil {
				return nil, err
			}
			return &ast.Assign{
				Loc:     t.From,
				LValues: lvalues,
				Exprs:   values,
				Define:  t.Type == TDefAssign,
			}, nil

		case TPlusEq, TMinusEq:
			if len(lvalues) != 1 {
				return nil, p.errf(tStmt.From, "expected 1 expression")
			}

			var op ast.BinaryType
			if t.Type == TPlusEq {
				op = ast.BinaryPlus
			} else {
				op = ast.BinaryMinus
			}
			value, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			return &ast.Assign{
				Loc:     t.From,
				LValues: lvalues,
				Exprs: []ast.AST{
					&ast.Binary{
						Loc:   t.From,
						Left:  lvalues[0],
						Op:    op,
						Right: value,
					},
				},
			}, nil

		case TPlusPlus, TMinusMinus:
			if len(lvalues) != 1 {
				return nil, p.errf(tStmt.From, "expected 1 expression")
			}

			var op ast.BinaryType
			if t.Type == TPlusPlus {
				op = ast.BinaryPlus
			} else {
				op = ast.BinaryMinus
			}
			return &ast.Assign{
				Loc:     t.From,
				LValues: lvalues,
				Exprs: []ast.AST{
					&ast.Binary{
						Loc:  t.From,
						Left: lvalues[0],
						Op:   op,
						Right: &ast.Constant{
							Loc:   t.From,
							Value: int32(1),
						},
					},
				},
			}, nil

		default:
			p.lexer.Unget(t)
			return nil, p.errf(t.From, "syntax error")
		}
	}
}

func (p *Parser) parseExprList() ([]ast.AST, error) {
	var list []ast.AST

	for {
		expr, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		list = append(list, expr)
		t, err := p.lexer.Get()
		if err != nil {
			return nil, err
		}
		if t.Type != TComma {
			p.lexer.Unget(t)
			break
		}
	}

	return list, nil
}

func (p *Parser) parseExpr() (ast.AST, error) {
	// Precedence Operator
	// -----------------------------
	//   5          * / % << >> & &^
	//   4          + - | ^
	//   3          == != < <= > >=
	//   2          &&
	//   1          ||
	return p.parseExprLogicalOr()
}

func (p *Parser) parseExprLogicalOr() (ast.AST, error) {
	left, err := p.parseExprLogicalAnd()
	if err != nil {
		return nil, err
	}
	for {
		t, err := p.lexer.Get()
		if err != nil {
			return nil, err
		}
		if t.Type != TOr {
			p.lexer.Unget(t)
			return left, nil
		}
		right, err := p.parseExprLogicalAnd()
		if err != nil {
			return nil, err
		}
		left = &ast.Binary{
			Loc:   t.From,
			Left:  left,
			Op:    t.Type.BinaryType(),
			Right: right,
		}
	}
}

func (p *Parser) parseExprLogicalAnd() (ast.AST, error) {
	left, err := p.parseExprComparative()
	if err != nil {
		return nil, err
	}
	for {
		t, err := p.lexer.Get()
		if err != nil {
			return nil, err
		}
		if t.Type != TAnd {
			p.lexer.Unget(t)
			return left, nil
		}
		right, err := p.parseExprComparative()
		if err != nil {
			return nil, err
		}
		left = &ast.Binary{
			Loc:   t.From,
			Left:  left,
			Op:    t.Type.BinaryType(),
			Right: right,
		}
	}
}

func (p *Parser) parseExprComparative() (ast.AST, error) {
	left, err := p.parseExprAdditive()
	if err != nil {
		return nil, err
	}
	for {
		t, err := p.lexer.Get()
		if err != nil {
			return nil, err
		}
		switch t.Type {
		case TEq, TNeq, TLt, TLe, TGt, TGe:
			right, err := p.parseExprAdditive()
			if err != nil {
				return nil, err
			}
			left = &ast.Binary{
				Loc:   t.From,
				Left:  left,
				Op:    t.Type.BinaryType(),
				Right: right,
			}

		default:
			p.lexer.Unget(t)
			return left, nil
		}
	}
}

func (p *Parser) parseExprAdditive() (ast.AST, error) {
	left, err := p.parseExprMultiplicative()
	if err != nil {
		return nil, err
	}
	for {
		t, err := p.lexer.Get()
		if err != nil {
			return nil, err
		}
		switch t.Type {
		case TPlus, TMinus, TBitOr, TBitXor:
			right, err := p.parseExprMultiplicative()
			if err != nil {
				return nil, err
			}
			left = &ast.Binary{
				Loc:   t.From,
				Left:  left,
				Op:    t.Type.BinaryType(),
				Right: right,
			}

		default:
			p.lexer.Unget(t)
			return left, nil
		}
	}
}

func (p *Parser) parseExprMultiplicative() (ast.AST, error) {
	left, err := p.parseExprPrimary()
	if err != nil {
		return nil, err
	}
	for {
		t, err := p.lexer.Get()
		if err != nil {
			return nil, err
		}
		switch t.Type {
		case TMult, TDiv, TMod, TLshift, TRshift, TBitAnd, TBitClear:
			right, err := p.parseExprPrimary()
			if err != nil {
				return nil, err
			}
			left = &ast.Binary{
				Loc:   t.From,
				Left:  left,
				Op:    t.Type.BinaryType(),
				Right: right,
			}

		default:
			p.lexer.Unget(t)
			return left, nil
		}
	}
}

// PrimaryExpr =
//     Operand |
//     Conversion |
//     MethodExpr |
//     PrimaryExpr Selector |
//     PrimaryExpr Index |
//     PrimaryExpr Slice |
//     PrimaryExpr TypeAssertion |
//     PrimaryExpr Arguments .
//
// Selector       = "." identifier .
// Index          = "[" Expression "]" .
// Slice          = "[" [ Expression ] ":" [ Expression ] "]" |
//                  "[" [ Expression ] ":" Expression ":" Expression "]" .
// TypeAssertion  = "." "(" Type ")" .
// Arguments      = "(" [ ( ExpressionList | Type [ "," ExpressionList ] ) [ "..." ] [ "," ] ] ")" .

func (p *Parser) parseExprPrimary() (ast.AST, error) {
	primary, err := p.parseOperand()
	if err != nil {
		return nil, err
	}

	for {
		t, err := p.lexer.Get()
		if err != nil {
			if err == io.EOF {
				return primary, nil
			}
			return nil, err
		}
		switch t.Type {
		case TDot:
			// Selector.
			return nil, fmt.Errorf("Selector not implemented yet")

		case TLBracket:
			var expr1, expr2 ast.AST

			n, err := p.lexer.Get()
			if err != nil {
				return nil, err
			}
			if n.Type != TColon {
				p.lexer.Unget(n)
				expr1, err = p.parseExpr()
				if err != nil {
					return nil, err
				}
				_, err = p.needToken(TColon)
				if err != nil {
					return nil, err
				}
			}
			n, err = p.lexer.Get()
			if err != nil {
				return nil, err
			}
			if n.Type != TRBracket {
				p.lexer.Unget(n)
				expr2, err = p.parseExpr()
				if err != nil {
					return nil, err
				}
				_, err = p.needToken(TRBracket)
				if err != nil {
					return nil, err
				}
			}
			return &ast.Slice{
				Loc:  primary.Location(),
				Expr: primary,
				From: expr1,
				To:   expr2,
			}, nil

		case TLParen:
			// Arguments.
			var arguments []ast.AST
			for {
				expr, err := p.parseExpr()
				if err != nil {
					return nil, err
				}
				arguments = append(arguments, expr)

				n, err := p.lexer.Get()
				if err != nil {
					return nil, err
				}
				if n.Type == TRParen {
					break
				} else if n.Type != TComma {
					return nil, p.errf(n.From, "unexpected token %s", n)
				}
			}
			vr, ok := primary.(*ast.VariableRef)
			if !ok {
				return nil, p.errf(primary.Location(),
					"non-function %s used as function", primary)
			}
			return &ast.Call{
				Loc:   primary.Location(),
				Name:  vr.Name,
				Exprs: arguments,
			}, nil

		default:
			p.lexer.Unget(t)
			return primary, nil
		}
	}
}

// Operand     = Literal | OperandName | "(" Expression ")" .
// Literal     = BasicLit | CompositeLit | FunctionLit .
// BasicLit    = int_lit | float_lit | imaginary_lit | rune_lit | string_lit .
// OperandName = identifier | QualifiedIdent .
//
// QualifiedIdent = PackageName "." identifier .

func (p *Parser) parseOperand() (ast.AST, error) {
	t, err := p.lexer.Get()
	if err != nil {
		return nil, err
	}
	switch t.Type {
	case TConstant: // Literal
		return &ast.Constant{
			Loc:   t.From,
			Value: t.ConstVal,
		}, nil

	case TIdentifier: // OperandName
		n, err := p.lexer.Get()
		if err != nil {
			return nil, err
		}
		if n.Type == TDot {
			id, err := p.needToken(TIdentifier)
			if err != nil {
				return nil, err
			}
			// QualifiedIdent.
			return &ast.VariableRef{
				Loc: t.From,
				Name: ast.Identifier{
					Package: t.StrVal,
					Name:    id.StrVal,
				},
			}, nil
		}
		// Identifier in current package.
		p.lexer.Unget(n)
		return &ast.VariableRef{
			Loc: t.From,
			Name: ast.Identifier{
				Name: t.StrVal,
			},
		}, nil

	case TLParen: // '(' Expression ')'
		expr, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		_, err = p.needToken(TRParen)
		if err != nil {
			return nil, err
		}
		return expr, nil

	default:
		p.lexer.Unget(t)
		return nil, p.errf(t.From,
			"unexpected token '%s' while parsing expression", t)
	}
}

// Type      = TypeName | TypeLit | "(" Type ")" .
// TypeName  = identifier | QualifiedIdent .
// TypeLit   = ArrayType | StructType | SliceType .
func (p *Parser) parseType() (*ast.TypeInfo, error) {
	t, err := p.lexer.Get()
	if err != nil {
		return nil, err
	}
	switch t.Type {
	case TIdentifier:
		var name string
		n, err := p.lexer.Get()
		if err != nil {
			return nil, err
		}
		if n.Type == TDot {
			n, err = p.lexer.Get()
			if err != nil {
				return nil, err
			}
			if n.Type == TIdentifier {
				name = n.StrVal
			} else {
				p.lexer.Unget(n)
			}
		} else {
			p.lexer.Unget(n)
		}
		var pkg string
		if len(name) > 0 {
			pkg = t.StrVal
		} else {
			name = t.StrVal
		}
		return &ast.TypeInfo{
			Type: ast.TypeName,
			Name: ast.Identifier{
				Package: pkg,
				Name:    name,
			},
		}, nil

	case TLBracket:
		n, err := p.lexer.Get()
		if err != nil {
			return nil, err
		}
		var length ast.AST
		if n.Type != TRBracket {
			p.lexer.Unget(n)
			length, err = p.parseExpr()
			if err != nil {
				return nil, err
			}
			_, err := p.needToken(TRBracket)
			if err != nil {
				return nil, err
			}
		}
		elType, err := p.parseType()
		if err != nil {
			return nil, err
		}
		if length != nil {
			return &ast.TypeInfo{
				Type:        ast.TypeArray,
				ElementType: elType,
				ArrayLength: length,
			}, nil
		}
		return &ast.TypeInfo{
			Type:        ast.TypeSlice,
			ElementType: elType,
		}, nil

	default:
		return nil, p.errf(t.From,
			"unexpected token '%s' while parsing type", t)
	}
}
