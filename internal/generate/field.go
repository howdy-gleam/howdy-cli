package generate

import (
	"fmt"
	"strings"
)

// Kind is a field type accepted on the command line.
type Kind string

const (
	String Kind = "string"
	Int    Kind = "int"
	Float  Kind = "float"
	Bool   Kind = "bool"
)

// Field is one column of the record besides id.
type Field struct {
	Name string
	Kind Kind

	database string
}

func parseField(arg string) (Field, error) {
	name, kind, ok := strings.Cut(arg, ":")
	if !ok {
		// A bare name is a string, as in Rails and Phoenix generators.
		kind = string(String)
	}
	if !identifier.MatchString(name) {
		return Field{}, fmt.Errorf("%q is not a valid field: write name:type with a lowercase name, for example age:int", arg)
	}
	if gleamKeywords[name] || sqlKeywords[name] || fieldNames[name] {
		return Field{}, fmt.Errorf("field %q is a reserved word in Gleam or SQL, or is used by the generated code; pick another name", name)
	}
	switch k := Kind(strings.ToLower(kind)); k {
	case String, Int, Float, Bool:
		return Field{Name: name, Kind: k}, nil
	case "text":
		return Field{Name: name, Kind: String}, nil
	case "integer":
		return Field{Name: name, Kind: Int}, nil
	case "boolean":
		return Field{Name: name, Kind: Bool}, nil
	}
	return Field{}, fmt.Errorf("field %q has unknown type %q: use string, int, float or bool", name, kind)
}

// GleamType is the field's type in the record.
func (f Field) GleamType() string {
	return map[Kind]string{String: "String", Int: "Int", Float: "Float", Bool: "Bool"}[f.Kind]
}

// JSONDecoder decodes the field from a request body.
func (f Field) JSONDecoder() string {
	if f.Kind == Float {
		// JSON writes 3.0 as 3, which decode.float alone rejects.
		return "number()"
	}
	return "decode." + string(f.Kind)
}

// JSONEncoder is the gleam/json function for the field.
func (f Field) JSONEncoder() string { return "json." + string(f.Kind) }

// RowDecoder decodes the column from a database row. SQLite has no boolean
// type, so booleans come back as 0 or 1.
func (f Field) RowDecoder() string {
	if f.Kind == Bool && f.database != "postgres" {
		return "decode.int |> decode.map(fn(n) { n != 0 })"
	}
	return "decode." + string(f.Kind)
}

// SQLValue wraps a Gleam expression as a query parameter.
func (f Field) SQLValue(expr string) string {
	if f.Kind == Float {
		return "value.GFloat(" + expr + ")"
	}
	return "sql." + string(f.Kind) + "(" + expr + ")"
}

// Column is the migration's column definition.
func (f Field) Column() string {
	module, kind := "sqlite", map[Kind]string{String: "Text", Int: "Integer", Float: "Real", Bool: "Integer"}[f.Kind]
	if f.database == "postgres" {
		module, kind = "pg", map[Kind]string{String: "Text", Int: "BigInt", Float: "DoublePrecision", Bool: "Boolean"}[f.Kind]
	}
	return fmt.Sprintf("%s.column(%q, %s.%s) |> %s.not_null", module, f.Name, module, kind, module)
}

// Rules are the validation rules the field starts with.
func (f Field) Rules() string {
	if f.Kind == String {
		return "[validate.trim(), validate.not_empty()]"
	}
	return "[]"
}

// Sample is a JSON value for the tests; Changed is a different one.
func (f Field) Sample() string  { return f.sample(false) }
func (f Field) Changed() string { return f.sample(true) }

func (f Field) sample(changed bool) string {
	switch f.Kind {
	case Int:
		return "json.int(" + pick(changed, "2", "1") + ")"
	case Float:
		return "json.float(" + pick(changed, "2.5", "1.5") + ")"
	case Bool:
		return "json.bool(" + pick(changed, "True", "False") + ")"
	}
	return fmt.Sprintf("json.string(%q)", pick(changed, "Changed ", "First ")+f.Name)
}

// SampleGleam and ChangedGleam are the same values as Gleam literals.
func (f Field) SampleGleam() string  { return unwrap(f.Sample()) }
func (f Field) ChangedGleam() string { return unwrap(f.Changed()) }

func pick(first bool, a, b string) string {
	if first {
		return a
	}
	return b
}

func unwrap(call string) string {
	return strings.TrimSuffix(call[strings.Index(call, "(")+1:], ")")
}

// Label is the field's name for people, e.g. "In stock".
func (f Field) Label() string { return humanize(f.Name, true) }

func humanize(name string, capital bool) string {
	s := strings.ReplaceAll(name, "_", " ")
	if capital {
		s = strings.ToUpper(s[:1]) + s[1:]
	}
	return s
}

// FormParse is the line of the pages' parse function that reads the field
// from a submitted form.
func (f Field) FormParse() string {
	switch f.Kind {
	case Int:
		return fmt.Sprintf("use %s <- pages.whole_number(form, %q)", f.Name, f.Name)
	case Float:
		return fmt.Sprintf("use %s <- pages.number(form, %q)", f.Name, f.Name)
	case Bool:
		return fmt.Sprintf("let %s = pages.checked(form, %q)", f.Name, f.Name)
	}
	return fmt.Sprintf("let %s = pages.value(form, %q)", f.Name, f.Name)
}

// FormValue is the field of record as the text an input holds.
func (f Field) FormValue(record string) string {
	v := record + "." + f.Name
	switch f.Kind {
	case Int:
		return "int.to_string(" + v + ")"
	case Float:
		return "float.to_string(" + v + ")"
	case Bool:
		return "case " + v + " {\n True -> \"on\"\n False -> \"\"\n }"
	}
	return v
}

// Display is the field of record as text on a page.
func (f Field) Display(record string) string {
	if f.Kind == Bool {
		return "yes_no(" + record + "." + f.Name + ")"
	}
	return f.FormValue(record)
}

// FormInput is the field's input element.
func (f Field) FormInput() string {
	attributes := map[Kind]string{
		String: `a.type_("text")`,
		Int:    `a.type_("number")`,
		Float:  `a.type_("number"), a.step("any")`,
	}
	if f.Kind == Bool {
		return fmt.Sprintf("pages.checkbox(form, %q, %q)", f.Name, f.Label())
	}
	return fmt.Sprintf("pages.field(form, errors, %q, %q, [%s])", f.Name, f.Label(), attributes[f.Kind])
}

// formText is the sample value as typed into a form; "" is an unticked
// checkbox, which browsers leave out.
func (f Field) formText(changed bool) string {
	switch f.Kind {
	case Bool:
		return pick(changed, "on", "")
	case String:
		return pick(changed, "Changed ", "First ") + f.Name
	}
	return unwrap(f.sample(changed))
}

// FormPair is the sample value as an entry of a Gleam list of form fields.
func (f Field) FormPair(changed bool) string {
	text := f.formText(changed)
	if f.Kind == Bool && text == "" {
		return ""
	}
	return fmt.Sprintf("#(%q, %q), ", f.Name, text)
}

// Shown is the sample value as the pages display it.
func (f Field) Shown(changed bool) string {
	if f.Kind == Bool {
		return pick(changed, "Yes", "No")
	}
	return f.formText(changed)
}

// InForm is what the edit form's HTML holds for the first sample value.
func (f Field) InForm() string {
	if f.Kind == Bool {
		return fmt.Sprintf(`name=\"%s\"`, f.Name)
	}
	return fmt.Sprintf(`value=\"%s\"`, f.formText(false))
}
