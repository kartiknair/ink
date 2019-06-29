package main

import (
	"fmt"
	"strings"
)

type Value interface {
	String() string
	Equals(Value) bool // deep, value equality
}

// utility func to get a consistent, language spec-compliant
//	string representation of numbers
// XXX: not the most reliable check for int because of int64 range
//	limitations, but works for now until we nail down Ink's number
//	spec more rigorously
func nToS(n float64) string {
	i := int64(n)
	if n == float64(i) {
		return fmt.Sprintf("%d", i)
	} else {
		return fmt.Sprintf("%.8f", n)
	}
}

// TODO: implement bytes literal and values, and make
//	file read/write APIs on that, rather than text

// TODO: implement exception handling / error values
//	let's do L3-style Result types that are composite values
//	with an error value returned with the return value.

// The EmptyValue is the value of the empty identifier.
//	it is globally unique and matches everything in equality.
type EmptyValue struct{}

func (v EmptyValue) String() string {
	return "_"
}

func (v EmptyValue) Equals(other Value) bool {
	return true
}

type NumberValue struct {
	val float64
}

func (v NumberValue) String() string {
	return nToS(v.val)
}

func (v NumberValue) Equals(other Value) bool {
	if _, isEmpty := other.(EmptyValue); isEmpty {
		return true
	}

	if ov, ok := other.(NumberValue); ok {
		return v.val == ov.val
	} else {
		return false
	}
}

type StringValue struct {
	val string
}

func (v StringValue) String() string {
	return v.val
}

func (v StringValue) Equals(other Value) bool {
	if _, isEmpty := other.(EmptyValue); isEmpty {
		return true
	}

	if ov, ok := other.(StringValue); ok {
		return v.val == ov.val
	} else {
		return false
	}
}

type BooleanValue struct {
	val bool
}

func (v BooleanValue) String() string {
	if v.val {
		return "true"
	} else {
		return "false"
	}
}

func (v BooleanValue) Equals(other Value) bool {
	if _, isEmpty := other.(EmptyValue); isEmpty {
		return true
	}

	if ov, ok := other.(BooleanValue); ok {
		return v.val == ov.val
	} else {
		return false
	}
}

type NullValue struct{}

func (v NullValue) String() string {
	return "()"
}

func (v NullValue) Equals(other Value) bool {
	if _, isEmpty := other.(EmptyValue); isEmpty {
		return true
	}

	_, ok := other.(NullValue)
	return ok
}

type CompositeValue struct {
	entries ValueTable
}

func (v CompositeValue) String() string {
	if len(v.entries) == 0 {
		return "{}"
	} else {
		entries := make([]string, 0)
		for key, ent := range v.entries {
			entries = append(
				entries,
				fmt.Sprintf("%s: %s", key, ent.String()),
			)
		}
		return fmt.Sprintf("{%s}", strings.Join(entries, ", "))
	}
}

func (v CompositeValue) Equals(other Value) bool {
	if _, isEmpty := other.(EmptyValue); isEmpty {
		return true
	}

	if ov, ok := other.(CompositeValue); ok {
		if len(v.entries) != len(ov.entries) {
			return false
		}

		for key, val := range v.entries {
			otherVal, prs := ov.entries[key]
			if prs && !val.Equals(otherVal) {
				return false
			}
		}
		return true
	} else {
		return false
	}
}

// XXX: for now, our GC heuristic is simply to dump/free
//	frames from functions that are no longer referenced in the
//	main context's frame, and keep all other frames, recursively descending.
// This is conservative and inefficient, but will get us started.
type FunctionValue struct {
	defNode     FunctionLiteralNode
	parentFrame *StackFrame
}

func (v FunctionValue) String() string {
	return v.defNode.String()
}

func (v FunctionValue) Equals(other Value) bool {
	if _, isEmpty := other.(EmptyValue); isEmpty {
		return true
	}

	if ov, ok := other.(FunctionValue); ok {
		// to compare structs containing slices, we really want
		//	a pointer comparison, not a value comparison
		return &v.defNode == &ov.defNode
	} else {
		return false
	}
}

type FunctionCallThunkValue struct {
	vt       ValueTable
	function FunctionValue
}

func (v FunctionCallThunkValue) String() string {
	return fmt.Sprintf("Tail Call Thunk of (%s)", v.function.String())
}

func (v FunctionCallThunkValue) Equals(other Value) bool {
	if _, isEmpty := other.(EmptyValue); isEmpty {
		return true
	}

	if ov, ok := other.(FunctionCallThunkValue); ok {
		// to compare structs containing slices, we really want
		//	a pointer comparison, not a value comparison
		return &v.vt == &ov.vt &&
			&v.function == &ov.function
	} else {
		return false
	}
}

func unwrapThunk(v Value) (Value, error) {
	thunk, isThunk := v.(FunctionCallThunkValue)
	// this effectively expands out a recursive structure (of thunks)
	//	into a for loop control structure
	for isThunk {
		frame := &StackFrame{
			parent: thunk.function.parentFrame,
			vt:     thunk.vt,
		}
		var err error
		v, err = thunk.function.defNode.body.Eval(frame, true)
		if err != nil {
			return nil, err
		}
		thunk, isThunk = v.(FunctionCallThunkValue)
	}

	return v, nil
}

type Node interface {
	String() string
	Eval(*StackFrame, bool) (Value, error)
	// TODO: func (n Node) prettyString() string - pretty-print AST
}

func (n UnaryExprNode) String() string {
	return fmt.Sprintf("Unary %s (%s)", n.operator.String(), n.operand.String())
}

func (n UnaryExprNode) Eval(frame *StackFrame, allowThunk bool) (Value, error) {
	switch n.operator.kind {
	case NegationOp:
		operand, err := n.operand.Eval(frame, false)
		if err != nil {
			return nil, err
		}

		switch o := operand.(type) {
		case NumberValue:
			return NumberValue{-o.val}, nil
		case BooleanValue:
			return BooleanValue{!o.val}, nil
		default:
			return nil, Err{
				ErrRuntime,
				fmt.Sprintf("cannot negate non-boolean and non-number value %s", o.String()),
			}
		}
	}

	logErrf(ErrAssert, "unrecognized unary operator %s", n)
	return nil, nil
}

func (n BinaryExprNode) String() string {
	var op = "??"
	switch n.operator.kind {
	case AddOp:
		op = "+"
	case SubtractOp:
		op = "-"
	case MultiplyOp:
		op = "*"
	case DivideOp:
		op = "/"
	case ModulusOp:
		op = "%"
	case GreaterThanOp:
		op = ">"
	case LessThanOp:
		op = "<"
	case EqualOp:
		op = "="
	case EqRefOp:
		op = "is"
	case DefineOp:
		op = ":="
	case AccessorOp:
		op = "."
	}
	return fmt.Sprintf("Binary (%s) %s (%s)",
		n.leftOperand.String(),
		op,
		n.rightOperand.String())
}

func operandToStringKey(rightOperand Node, frame *StackFrame) (string, error) {
	switch right := rightOperand.(type) {
	case IdentifierNode:
		return right.val, nil

	case StringLiteralNode:
		return right.val, nil

	case NumberLiteralNode:
		return nToS(right.val), nil

	default:
		rightEvaluatedValue, err := rightOperand.Eval(frame, false)
		if err != nil {
			return "", err
		}

		switch rv := rightEvaluatedValue.(type) {
		case StringValue:
			return rv.val, nil
		case NumberValue:
			return nToS(rv.val), nil
		default:
			return "", Err{
				ErrRuntime,
				fmt.Sprintf("cannot access invalid property name %s of a composite value",
					rightEvaluatedValue.String()),
			}
		}
	}
}

func (n BinaryExprNode) Eval(frame *StackFrame, allowThunk bool) (Value, error) {
	if n.operator.kind == DefineOp {
		if leftIdent, okIdent := n.leftOperand.(IdentifierNode); okIdent {
			rightValue, err := n.rightOperand.Eval(frame, false)
			if err != nil {
				return nil, err
			}

			frame.setValue(leftIdent.val, rightValue)
			return rightValue, nil
		} else if leftAccess, okAccess := n.leftOperand.(BinaryExprNode); okAccess &&
			leftAccess.operator.kind == AccessorOp {

			leftObject, err := leftAccess.leftOperand.Eval(frame, false)
			if err != nil {
				return nil, err
			}

			leftKey, err := operandToStringKey(leftAccess.rightOperand, frame)
			if err != nil {
				return nil, err
			}

			if leftObjectComposite, isComposite := leftObject.(CompositeValue); isComposite {
				rightValue, err := n.rightOperand.Eval(frame, false)
				if err != nil {
					return nil, err
				}

				leftObjectComposite.entries[leftKey] = rightValue
				return rightValue, nil
			} else {
				return nil, Err{
					ErrRuntime,
					fmt.Sprintf("cannot set property of a non-composite value %s",
						leftObject.String()),
				}
			}
		} else {
			left, _ := n.leftOperand.Eval(frame, false)
			return nil, Err{
				ErrRuntime,
				fmt.Sprintf("cannot assign value to non-identifier %s", left.String()),
			}
		}
	} else if n.operator.kind == AccessorOp {
		leftValue, err := n.leftOperand.Eval(frame, false)
		if err != nil {
			return nil, err
		}

		rightValueStr, err := operandToStringKey(n.rightOperand, frame)
		if err != nil {
			return nil, err
		}

		if leftValueComposite, isComposite := leftValue.(CompositeValue); isComposite {
			v, prs := leftValueComposite.entries[rightValueStr]
			if prs {
				return v, nil
			} else {
				return NullValue{}, nil
			}
		} else {
			return nil, Err{
				ErrRuntime,
				fmt.Sprintf("cannot access property of a non-composite value%s",
					leftValue),
			}
		}
	}

	leftValue, err := n.leftOperand.Eval(frame, false)
	if err != nil {
		return nil, err
	}
	rightValue, err := n.rightOperand.Eval(frame, false)
	if err != nil {
		return nil, err
	}

	switch n.operator.kind {
	case AddOp:
		switch left := leftValue.(type) {
		case NumberValue:
			if right, ok := rightValue.(NumberValue); ok {
				return NumberValue{left.val + right.val}, nil
			}
		case StringValue:
			if right, ok := rightValue.(StringValue); ok {
				return StringValue{left.val + right.val}, nil
			}
		case BooleanValue:
			if right, ok := rightValue.(BooleanValue); ok {
				return BooleanValue{left.val || right.val}, nil
			}
		}
		return nil, Err{
			ErrRuntime,
			fmt.Sprintf("values %s and %s do not support addition",
				leftValue, rightValue),
		}
	case SubtractOp:
		switch left := leftValue.(type) {
		case NumberValue:
			if right, ok := rightValue.(NumberValue); ok {
				return NumberValue{left.val - right.val}, nil
			}
		}
		return nil, Err{
			ErrRuntime,
			fmt.Sprintf("values %s and %s do not support subtraction",
				leftValue, rightValue),
		}
	case MultiplyOp:
		switch left := leftValue.(type) {
		case NumberValue:
			if right, ok := rightValue.(NumberValue); ok {
				return NumberValue{left.val * right.val}, nil
			}
		case BooleanValue:
			if right, ok := rightValue.(BooleanValue); ok {
				return BooleanValue{left.val && right.val}, nil
			}
		}
		return nil, Err{
			ErrRuntime,
			fmt.Sprintf("values %s and %s do not support multiplication",
				leftValue, rightValue),
		}
	case DivideOp:
		switch left := leftValue.(type) {
		case NumberValue:
			if right, ok := rightValue.(NumberValue); ok {
				return NumberValue{left.val / right.val}, nil
			}
		}
		return nil, Err{
			ErrRuntime,
			fmt.Sprintf("values %s and %s do not support division",
				leftValue, rightValue),
		}
	case ModulusOp:
		switch left := leftValue.(type) {
		case NumberValue:
			if right, ok := rightValue.(NumberValue); ok {
				if right.val == float64(int64(right.val)) {
					return NumberValue{float64(
						int(left.val) % int(right.val),
					)}, nil
				} else {
					return nil, Err{
						ErrRuntime,
						fmt.Sprintf("cannot take modulus of non-integer value %s", nToS(right.val)),
					}
				}
			}
		}
		return nil, Err{
			ErrRuntime,
			fmt.Sprintf("values %s and %s do not support modulus",
				leftValue, rightValue),
		}
	case GreaterThanOp:
		switch left := leftValue.(type) {
		case NumberValue:
			if right, ok := rightValue.(NumberValue); ok {
				return BooleanValue{left.val > right.val}, nil
			}
		case StringValue:
			if right, ok := rightValue.(StringValue); ok {
				return BooleanValue{left.val > right.val}, nil
			}
		}
		return nil, Err{
			ErrRuntime,
			fmt.Sprintf("values %s and %s do not support comparison",
				leftValue, rightValue),
		}
	case LessThanOp:
		switch left := leftValue.(type) {
		case NumberValue:
			if right, ok := rightValue.(NumberValue); ok {
				return BooleanValue{left.val < right.val}, nil
			}
		case StringValue:
			if right, ok := rightValue.(StringValue); ok {
				return BooleanValue{left.val < right.val}, nil
			}
		}
		return nil, Err{
			ErrRuntime,
			fmt.Sprintf("values %s and %s do not support comparison",
				leftValue, rightValue),
		}
	case EqualOp:
		return BooleanValue{leftValue.Equals(rightValue)}, nil
	case EqRefOp:
		// XXX: this is probably not 100% true. To make a 100% faithful
		//	implementation would require us to roll our own
		//	name table, which isn't a short-term todo item.
		return BooleanValue{leftValue == rightValue}, nil
	}

	logErrf(ErrAssert, "unknown binary operator %s", n.String())
	return nil, err
}

func (n FunctionCallNode) String() string {
	if len(n.arguments) == 0 {
		return fmt.Sprintf("Call (%s) on ()", n.function)
	} else {
		args := n.arguments[0].String()
		for _, a := range n.arguments[1:] {
			args += ", " + a.String()
		}
		return fmt.Sprintf("Call (%s) on (%s)", n.function, args)
	}
}

func (n FunctionCallNode) Eval(frame *StackFrame, allowThunk bool) (Value, error) {
	fn, err := n.function.Eval(frame, false)
	if err != nil {
		return nil, err
	}

	if fn == nil {
		return nil, Err{
			ErrRuntime,
			fmt.Sprintf("attempted to call an unknown function at %s",
				n.function.String()),
		}
	}

	if fnt, isFunc := fn.(FunctionValue); isFunc {
		argResults := make([]Value, len(n.arguments))
		for i, arg := range n.arguments {
			argResults[i], err = arg.Eval(frame, false)
			if err != nil {
				return nil, err
			}
		}

		argValueTable := ValueTable{}
		for i, identNode := range fnt.defNode.arguments {
			if len(argResults) > i {
				argValueTable[identNode.val] = argResults[i]
			}
		}

		// TCO: used for evaluating expressions that may be in tail positions
		//	at the end of Nodes whose evaluation allocates another StackFrame
		//	like ExpressionList and FunctionLiteral's body
		returnThunk := FunctionCallThunkValue{
			vt:       argValueTable,
			function: fnt,
		}
		if allowThunk {
			return returnThunk, nil
		} else {
			return unwrapThunk(returnThunk)
		}
	} else if fnt, isNativeFunc := fn.(NativeFunctionValue); isNativeFunc {
		// cannot optimize native function calls
		argResults := make([]Value, len(n.arguments))
		for i, arg := range n.arguments {
			argResults[i], err = arg.Eval(frame, allowThunk)
			if err != nil {
				return nil, err
			}
		}
		return fnt.exec(argResults)
	} else {
		return nil, Err{
			ErrRuntime,
			fmt.Sprintf("attempted to call a non-function value %s", fn.String()),
		}
	}
}

func (n MatchClauseNode) String() string {
	return fmt.Sprintf("Clause (%s) -> (%s)",
		n.target.String(),
		n.expression.String())
}

func (n MatchClauseNode) Eval(frame *StackFrame, allowThunk bool) (Value, error) {
	logErrf(ErrAssert, "cannot Eval a MatchClauseNode")
	return nil, nil
}

func (n MatchExprNode) String() string {
	if len(n.clauses) == 0 {
		return fmt.Sprintf("Match on (%s) to {}", n.condition.String())
	} else {
		clauses := n.clauses[0].String()
		for _, c := range n.clauses[1:] {
			clauses += ", " + c.String()
		}
		return fmt.Sprintf("Match on (%s) to {%s}",
			n.condition.String(),
			clauses)
	}
}

func (n MatchExprNode) Eval(frame *StackFrame, allowThunk bool) (Value, error) {
	conditionVal, err := n.condition.Eval(frame, false)
	if err != nil {
		return nil, err
	}

	for _, cl := range n.clauses {
		targetVal, err := cl.target.Eval(frame, false)
		if err != nil {
			return nil, err
		}

		if conditionVal.Equals(targetVal) {
			rv, err := cl.expression.Eval(frame, allowThunk)
			if err != nil {
				return nil, err
			}
			// match expression clauses are tail call optimized,
			//	so return a maybe ThunkValue

			return rv, nil
		}
	}

	return NullValue{}, nil
}

func (n ExpressionListNode) String() string {
	if len(n.expressions) == 0 {
		return "Expression List ()"
	} else {
		exprs := n.expressions[0].String()
		for _, expr := range n.expressions[1:] {
			exprs += ", " + expr.String()
		}
		return fmt.Sprintf("Expression List (%s)", exprs)
	}
}

func (n ExpressionListNode) Eval(frame *StackFrame, allowThunk bool) (Value, error) {
	length := len(n.expressions)

	if length == 0 {
		return NullValue{}, nil
	}

	callFrame := &StackFrame{
		parent: frame,
		vt:     ValueTable{},
	}
	for _, expr := range n.expressions[:length-1] {
		_, err := expr.Eval(callFrame, false)
		if err != nil {
			return nil, err
		}
	}

	// return values of expression lists are tail call optimized,
	//	so return a maybe ThunkValue
	return n.expressions[length-1].Eval(callFrame, allowThunk)
}

func (n EmptyIdentifierNode) String() string {
	return "Empty Identifier"
}

func (n EmptyIdentifierNode) Eval(frame *StackFrame, allowThunk bool) (Value, error) {
	return EmptyValue{}, nil
}

func (n IdentifierNode) String() string {
	return fmt.Sprintf("Identifier '%s'", n.val)
}

func (n IdentifierNode) Eval(frame *StackFrame, allowThunk bool) (Value, error) {
	val, prs := frame.getValue(n.val)
	if !prs {
		return nil, Err{
			ErrRuntime,
			fmt.Sprintf("%s is not defined", n.val),
		}
	}
	return val, nil
}

func (n NumberLiteralNode) String() string {
	return fmt.Sprintf("Number %s", nToS(n.val))
}

func (n NumberLiteralNode) Eval(frame *StackFrame, allowThunk bool) (Value, error) {
	return NumberValue{n.val}, nil
}

func (n StringLiteralNode) String() string {
	return fmt.Sprintf("String '%s'", n.val)
}

func (n StringLiteralNode) Eval(frame *StackFrame, allowThunk bool) (Value, error) {
	return StringValue{n.val}, nil
}

func (n BooleanLiteralNode) String() string {
	return fmt.Sprintf("Boolean %t", n.val)
}

func (n BooleanLiteralNode) Eval(frame *StackFrame, allowThunk bool) (Value, error) {
	return BooleanValue{n.val}, nil
}

func (n ObjectLiteralNode) String() string {
	if len(n.entries) == 0 {
		return fmt.Sprintf("Object {}")
	} else {
		entries := n.entries[0].String()
		for _, e := range n.entries[1:] {
			entries += ", " + e.String()
		}
		return fmt.Sprintf("Object {%s}", entries)
	}
}

func (n ObjectLiteralNode) Eval(frame *StackFrame, allowThunk bool) (Value, error) {
	obj := CompositeValue{
		entries: ValueTable{},
	}
	for _, entry := range n.entries {
		keyStr, err := operandToStringKey(entry.key, frame)
		if err != nil {
			return nil, err
		}

		obj.entries[keyStr], err = entry.val.Eval(frame, false)
		if err != nil {
			return nil, err
		}
	}
	return obj, nil
}

func (n ObjectEntryNode) String() string {
	return fmt.Sprintf("Object Entry (%s): (%s)", n.key.String(), n.val.String())
}

func (n ObjectEntryNode) Eval(frame *StackFrame, allowThunk bool) (Value, error) {
	logErrf(ErrAssert, "cannot Eval an ObjectEntryNode")
	return nil, nil
}

func (n ListLiteralNode) String() string {
	if len(n.vals) == 0 {
		return fmt.Sprintf("List []")
	} else {
		vals := n.vals[0].String()
		for _, v := range n.vals[1:] {
			vals += ", " + v.String()
		}
		return fmt.Sprintf("List [%s]", vals)
	}
}

func (n ListLiteralNode) Eval(frame *StackFrame, allowThunk bool) (Value, error) {
	listVal := CompositeValue{
		entries: ValueTable{},
	}

	for i, n := range n.vals {
		var err error
		listVal.entries[nToS(float64(i))], err = n.Eval(frame, false)
		if err != nil {
			return nil, err
		}
	}

	return listVal, nil
}

func (n FunctionLiteralNode) String() string {
	if len(n.arguments) == 0 {
		return fmt.Sprintf("Function () => (%s)", n.body.String())
	} else {
		args := n.arguments[0].String()
		for _, a := range n.arguments[1:] {
			args += ", " + a.String()
		}
		return fmt.Sprintf("Function (%s) => (%s)", args, n.body.String())
	}
}

func (n FunctionLiteralNode) Eval(frame *StackFrame, allowThunk bool) (Value, error) {
	return FunctionValue{
		defNode:     n,
		parentFrame: frame,
	}, nil
}

type ValueTable map[string]Value

type StackFrame struct {
	parent *StackFrame
	vt     ValueTable
}

func (sh *StackFrame) getValue(name string) (Value, bool) {
	val, ok := sh.vt[name]
	if ok {
		return val, true
	} else if sh.parent != nil {
		return sh.parent.getValue(name)
	} else {
		return NullValue{}, false
	}
}

func (sh *StackFrame) setValue(name string, val Value) {
	sh.vt[name] = val
}

func (sh *StackFrame) String() string {
	return fmt.Sprintf("frame: %s --prnt-> (%s)", sh.vt, sh.parent)
}

type Context struct {
	Frame *StackFrame
}

func (ctx *Context) Dump() {
	logDebug("frame dump ->", ctx.Frame.String())
}

func (ctx *Context) Init() {
	ctx.Frame = &StackFrame{
		parent: nil,
		vt:     ValueTable{},
	}
	ctx.LoadEnvironment()
}

func (ctx *Context) Eval(
	nodes <-chan Node,
	values chan<- Value,
	errors chan<- Err,
	dumpFrame bool,
) {
	for node := range nodes {
		val, err := node.Eval(ctx.Frame, false)
		if err != nil {
			e, isErr := err.(Err)
			if isErr {
				errors <- e
			} else {
				logErrf(ErrAssert, "error raised that was not of type Err -> %s",
					err.Error())
			}
			close(values)
			close(errors)
			return
		}
		values <- val
	}

	if dumpFrame {
		ctx.Dump()
	}

	close(values)
	close(errors)
}

func combineChan(c1, c2, c3 <-chan Err) <-chan Err {
	errors := make(chan Err)
	go func() {
		for {
			select {
			case e, ok := <-c1:
				if ok {
					errors <- e
				} else {
					c1 = nil
				}
			case e, ok := <-c2:
				if ok {
					errors <- e
				} else {
					c2 = nil
				}
			case e, ok := <-c3:
				if ok {
					errors <- e
				} else {
					c3 = nil
				}
			}
			if c1 == nil && c2 == nil && c3 == nil {
				close(errors)
				return
			}
		}
	}()
	return errors
}

func (ctx *Context) ExecStream(
	debugLex, debugParse, dump bool,
) (chan<- rune, <-chan Value, <-chan Err) {
	input := make(chan rune)
	tokens := make(chan Tok)
	nodes := make(chan Node)
	values := make(chan Value)

	e1 := make(chan Err)
	e2 := make(chan Err)
	e3 := make(chan Err)

	go Tokenize(input, tokens, e1, debugLex)
	go Parse(tokens, nodes, e2, debugParse)
	go ctx.Eval(nodes, values, e3, dump)

	return input, values, combineChan(e1, e2, e3)
}
