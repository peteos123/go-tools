package pkg

// whatever
func foo() {}

// Foo is amazing
func Foo() {}

// Whatever //@ diag(`comment on exported function`)
func Bar() {}

type T struct{}

// Whatever
func (T) foo() {}

// Foo is amazing
func (T) Foo() {}

// Whatever //@ diag(`comment on exported method`)
func (T) Bar() {}

// Deprecated: don't use.
func (T) Dep() {}

//
func Qux() {} // this is fine, because "no comment" and "empty comment" are treated the same

//
// Meow is amazing.
//
// godoc allows this style, because ast.CommentGroup.Text strips whitespace.
// We currently make no effort to flag it.
//
func Meow() {}

//some:directive
func F1() {} // we pretend that directives aren't part of the doc string, just like godoc in Go 1.15+ does

//some:directive
// F2 is amazing
func F2() {}

//some:directive //@ diag(`comment on exported function`)
// Whatever
func F3() {}

// Deprecated: don't use.
func F4() {}

//some:directive
// Deprecated: don't use.
func F5() {}

// wrong comment yo. //@diag (`comment on exported function`)
//
// Deprecated: don't use.
func F6() {}
