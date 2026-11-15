package emit

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"kogo/check"
	"kogo/syntax"
)

// The naming rule (decisions.md). A top-level Go name is the Kogo
// name with its first letter in upper case, so that a driver can call
// it; a nested type joins its outer one with _, and anything with a
// receiver follows the receiver's type: Booking_SupplierPay. Where
// two declarations of a package come out the same, each fun among
// them is spelled in full, with its receiver's type arguments and its
// parameters' types, and each val adds _val; a type keeps its name.

// export gives a Kogo name as a Go name that other packages can use
// and that no local has: it begins with an upper-case letter and does
// not end with _.
func export(name string) string {
	r, n := utf8.DecodeRuneInString(name)
	if up := unicode.ToUpper(r); unicode.IsUpper(up) {
		name = string(up) + name[n:]
	} else {
		name = "X" + name
	}
	if strings.HasSuffix(name, "_") {
		name += "X"
	}
	return name
}

func typeName(d *check.TypeDecl) string { return export(strings.ReplaceAll(d.Name, ".", "_")) }

// head names a receiver's type without its type arguments.
func head(t check.Type) string {
	switch t := t.(type) {
	case *check.Named:
		return typeName(t.Decl)
	case *check.Nullable:
		return head(t.Elem)
	case *check.TypeParam:
		return export(t.Name)
	}
	return "Function"
}

// spell writes types in full, as part of a name: ListT, IntOrNull,
// TToInt for (T) -> Int.
func spell(ts ...check.Type) string {
	s := ""
	for _, t := range ts {
		switch t := t.(type) {
		case *check.Named:
			s += export(strings.ReplaceAll(t.Decl.Name, ".", "")) + spell(t.Args...)
		case *check.Nullable:
			s += spell(t.Elem) + "OrNull"
		case *check.TypeParam:
			s += export(t.Name)
		case *check.FuncType:
			if t.Receiver != nil {
				s += spell(t.Receiver)
			}
			s += spell(t.Params...) + "To" + spell(t.Result)
		}
	}
	return s
}

// funNames gives a fun's or an extension val's short name and its
// name in full; object is the external object that holds it, if one
// does.
func funNames(f *check.Func, object string) (short, full string) {
	short, full = export(f.Name), export(f.Name)
	if f.Receiver != nil {
		short, full = head(f.Receiver)+"_"+short, spell(f.Receiver)+"_"+full
	} else if object != "" {
		short, full = object+"_"+short, object+"_"+full
	}
	switch {
	case f.Kind == check.Getter:
		return short, full + "_val"
	case len(f.Params) == 0:
		return short, full + "_fun"
	}
	for _, p := range f.Params {
		full += "_" + spell(p.Type)
	}
	return short, full
}

// name gives every top-level declaration of files its Go name.
func (e *emitter) name(files []*syntax.File) {
	type decl struct {
		obj         any
		at          syntax.Node
		file        *syntax.File
		short, full string
	}
	var pkgs []string
	decls := map[string][]*decl{}
	number := map[string]int{}
	for _, f := range files {
		pkg := f.Package.Name
		if number[pkg]++; number[pkg] == 1 {
			pkgs = append(pkgs, pkg)
		}
		add := func(obj any, at syntax.Node, visibility, short, full string) {
			if visibility == "private" { // this file's alone: another file may have one of the name
				private := "_f" + strconv.Itoa(number[pkg])
				short, full = short+private, full+private
			}
			decls[pkg] = append(decls[pkg], &decl{obj, at, f, short, full})
		}
		var class func(d *syntax.ClassDecl)
		class = func(d *syntax.ClassDecl) {
			td := e.info.Classes[d]
			if td == nil {
				return
			}
			n := typeName(td)
			if d.Kind == "external object" {
				for _, m := range td.Methods {
					short, full := funNames(m, n)
					add(m, m.Decl, "", short, full)
				}
				return
			}
			add(td, d, d.Mods.Visibility, n, n)
			for _, v := range td.Entries {
				add(v, v.Decl, d.Mods.Visibility, n+"_"+v.Name, n+"_"+v.Name)
			}
			for _, m := range d.Body {
				if nested, ok := m.(*syntax.ClassDecl); ok {
					class(nested)
				}
			}
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *syntax.ClassDecl:
				class(d)
			case *syntax.FunDecl:
				short, full := funNames(e.info.Funcs[d], "")
				add(e.info.Funcs[d], d, d.Mods.Visibility, short, full)
			case *syntax.ValDecl:
				if fn := e.info.Funcs[d]; fn != nil {
					short, full := funNames(fn, "")
					add(fn, d, d.Mods.Visibility, short, full)
				} else if vs := e.info.Vars[d]; vs != nil {
					add(vs[0], d, d.Mods.Visibility, export(d.Name), export(d.Name)+"_val")
				}
			}
		}
	}
	for _, pkg := range pkgs {
		shared := map[string]int{}
		for _, d := range decls[pkg] {
			shared[d.short]++
		}
		taken := map[string]bool{}
		for _, d := range decls[pkg] {
			n := d.short
			if shared[n] > 1 {
				n = d.full
			}
			if taken[n] {
				e.file = d.file
				e.refuse(d.at, "go-name", "two declarations of package %s are both %s in Go, even spelled with their types: rename one", pkg, n)
			}
			taken[n], e.names[d.obj] = true, n
		}
	}
}
