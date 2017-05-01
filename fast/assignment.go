/*
 * gomacro - A Go interpreter with Lisp-like macros
 *
 * Copyright (C) 2017 Massimiliano Ghilardi
 *
 *     This program is free software you can redistribute it and/or modify
 *     it under the terms of the GNU General Public License as published by
 *     the Free Software Foundation, either version 3 of the License, or
 *     (at your option) any later version.
 *
 *     This program is distributed in the hope that it will be useful,
 *     but WITHOUT ANY WARRANTY; without even the implied warranty of
 *     MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 *     GNU General Public License for more details.
 *
 *     You should have received a copy of the GNU General Public License
 *     along with this program.  If not, see <http//www.gnu.org/licenses/>.
 *
 * declaration.go
 *
 *  Created on Apr 01, 2017
 *      Author Massimiliano Ghilardi
 */

package fast

import (
	"go/ast"
	"go/token"
	r "reflect"
)

// Assign compiles an *ast.AssignStmt into an assignment to one or more place
func (c *Comp) Assign(node *ast.AssignStmt) {
	c.Pos = node.Pos()

	lhs, rhs := node.Lhs, node.Rhs
	if node.Tok == token.DEFINE {
		c.DeclVarsShort(lhs, rhs)
		return
	}
	ln, rn := len(lhs), len(rhs)
	if ln > 1 && rn == 1 {
		c.Errorf("unimplemented: assignment of multiple places with a single multi-valued expression: %v", node)
	} else if ln != rn {
		c.Errorf("invalid assignment, cannot assign %d values to %d places: %v", node)
	} else {
		for i := range lhs {
			c.Assign1(lhs[i], node.Tok, rhs[i])
		}
	}
}

// Assign1 compiles a single assignment to a place
func (c *Comp) Assign1(lhs ast.Expr, op token.Token, rhs ast.Expr) {
	if lhs != nil {
		c.Pos = lhs.Pos()
	}
	node := &ast.AssignStmt{Lhs: []ast.Expr{lhs}, Tok: op, Rhs: []ast.Expr{rhs}} // only for nice error messages

	place := c.Place(lhs)
	init := c.Expr1(rhs)

	panicking := true
	defer func() {
		if !panicking {
			return
		}
		rec := recover()
		c.Errorf("error compiling assignment: %v\n    %v", node, rec)
	}()
	if place.IsVar() {
		c.SetVar(&place.Var, op, init)
	} else {
		c.SetPlace(place, op, init)
	}
	panicking = false
}

// LookupVar compiles the left-hand-side of an assignment, in case it's an identifier (i.e. a variable name)
func (c *Comp) LookupVar(name string) *Var {
	if name == "_" {
		return &Var{}
	}
	sym := c.Resolve(name)
	return sym.AsVar(PlaceSettable)
}

// Place compiles the left-hand-side of an assignment
func (c *Comp) Place(node ast.Expr) *Place {
	return c.placeOrAddress(node, false)
}

// PlaceOrAddress compiles the left-hand-side of an assignment or the location of an address-of
func (c *Comp) placeOrAddress(in ast.Expr, opt PlaceOption) *Place {
	for {
		switch node := in.(type) {
		case *ast.ParenExpr:
			in = node.X
			continue
		case *ast.Ident:
			return c.IdentPlace(node.Name, opt)
		case *ast.IndexExpr:
			return c.IndexPlace(node, opt)
		case *ast.StarExpr:
			e := c.Expr1(node.X)
			if e.Const() {
				c.Errorf("%s a constant: %v <%v>", opt, node, e.Type)
				return nil
			} else if e.Sym != nil && opt == PlaceAddress {
				// optimize &*variable -> variable: it's already the address we want,
				// remember to dereference its type
				//
				// we cannot do this optimization when opt == PlaceSettable,
				// because in such case the code to compile is *variable - not an identifier
				va := *e.Sym.AsVar(opt)
				va.Type = va.Type.Elem()
				return &Place{Var: va}
			} else {
				// e.Fun is already the address we want,
				// remember to dereference its type
				t := e.Type.Elem()
				addr := e.AsX1()
				fun := func(env *Env) r.Value {
					return addr(env).Elem()
				}
				return &Place{Var: Var{Type: t}, Fun: fun, Addr: addr}
			}
		case *ast.SelectorExpr:
			return c.SelectorPlace(node, opt)
		default:
			c.Errorf("%s: %v", opt, in)
			return nil
		}
	}
}

// placeForSideEffects compiles the left-hand-side of a do-nothing assignment,
// as for example *addressOfInt() += 0, in order to apply its side effects
func (c *Comp) placeForSideEffects(place *Place) {
	if place.IsVar() {
		return
	}
	var ret Stmt
	fun := place.Fun
	if mapkey := place.MapKey; mapkey != nil {
		ret = func(env *Env) (Stmt, *Env) {
			fun(env)
			mapkey(env)
			// no need to call obj.MapIndex(key): it has no side effects and cannot panic.
			// obj := fun(env)
			// key := mapkey(env)
			// obj.MapIndex(key)
			env.IP++
			return env.Code[env.IP], env
		}
	} else {
		ret = func(env *Env) (Stmt, *Env) {
			fun(env)
			env.IP++
			return env.Code[env.IP], env
		}
	}
	c.Code.Append(ret)
}
