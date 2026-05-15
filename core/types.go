// Package core contains the MAXI intermediate representation (IR) types,
// error codes, and the global schema registry.
//
// Key types:
//
//   - [MaxiSchema] — the parsed schema: a collection of [MaxiTypeDef] values
//   - [MaxiTypeDef] — one type definition (alias, name, fields, parents)
//   - [MaxiFieldDef] — one field within a type (name, typeExpr, constraints, default)
//   - [MaxiRecord] — one parsed record (alias + positional values)
//   - [MaxiParseResult] — the complete output of a parse run
//   - [ParseOptions] — configures validation behaviour
//   - [MaxiError] — structured error with an error code (E101–E603)
//
// The registry functions [RegisterMaxiSchema] / [GetMaxiSchema] let you
// associate a [MaxiTypeDef] with a Go struct type using maxi: struct tags or
// explicit registration.
package core

import "strings"

type ConstraintType string

const (
	ConstraintRequired        ConstraintType = "required"
	ConstraintID              ConstraintType = "id"
	ConstraintComparison      ConstraintType = "comparison"
	ConstraintPattern         ConstraintType = "pattern"
	ConstraintMime            ConstraintType = "mime"
	ConstraintDecimalPrecision ConstraintType = "decimal-precision"
	ConstraintExactLength     ConstraintType = "exact-length"
)

type ParsedConstraint struct {
	Type     ConstraintType `json:"type"`
	Operator string         `json:"operator,omitempty"`
	Value    any            `json:"value,omitempty"`
}

type MaxiFieldDef struct {
	Name               string             `json:"name"`
	TypeExpr           string             `json:"typeExpr,omitempty"`
	Annotation         string             `json:"annotation,omitempty"`
	Constraints        []ParsedConstraint `json:"constraints,omitempty"`
	ElementConstraints []ParsedConstraint `json:"elementConstraints,omitempty"`
	DefaultValue any `json:"defaultValue,omitempty"`

	EnumValues []string `json:"-"`
	EnumAliases map[string]string `json:"-"`}

func (f *MaxiFieldDef) IsRequired() bool {
	for _, c := range f.Constraints {
		if c.Type == ConstraintRequired {
			return true
		}
	}
	return false
}

func (f *MaxiFieldDef) IsID() bool {
	for _, c := range f.Constraints {
		if c.Type == ConstraintID {
			return true
		}
	}
	return false
}

type MaxiTypeDef struct {
	Alias string `json:"alias"`
	Name string `json:"name,omitempty"`
	Parents []string `json:"parents,omitempty"`
	Fields []*MaxiFieldDef `json:"fields"`

	InheritanceResolved bool `json:"-"`

	idFieldIndex        int
	hasRuntimeConstraints bool
	requiredFlags       []bool
}

func (t *MaxiTypeDef) AddField(f *MaxiFieldDef) {
	t.Fields = append(t.Fields, f)
	t.invalidateCache()
}

func (t *MaxiTypeDef) invalidateCache() {
	t.idFieldIndex = -2
	t.requiredFlags = nil
}

func (t *MaxiTypeDef) ensureCache() {
	if t.idFieldIndex != -2 {
		return
	}
	n := len(t.Fields)

	t.idFieldIndex = -1
	for i, f := range t.Fields {
		if f.IsID() {
			t.idFieldIndex = i
			break
		}
	}
	if t.idFieldIndex == -1 {
		for i, f := range t.Fields {
			if f.Name == "id" {
				t.idFieldIndex = i
				break
			}
		}
	}
	if t.idFieldIndex == -1 {
		for i, f := range t.Fields {
			if strings.HasSuffix(f.Name, "_id") {
				t.idFieldIndex = i
				break
			}
		}
	}

	t.requiredFlags = make([]bool, n)
	for i, f := range t.Fields {
		t.requiredFlags[i] = f.IsRequired()
	}

	t.hasRuntimeConstraints = false
	for _, f := range t.Fields {
		for _, c := range f.Constraints {
			if c.Type == ConstraintComparison || c.Type == ConstraintPattern || c.Type == ConstraintExactLength {
				t.hasRuntimeConstraints = true
				break
			}
		}
	}
}

func (t *MaxiTypeDef) IDField() *MaxiFieldDef {
	t.ensureCache()
	if t.idFieldIndex >= 0 {
		return t.Fields[t.idFieldIndex]
	}
	return nil
}

func (t *MaxiTypeDef) IDFieldIndex() int {
	t.ensureCache()
	return t.idFieldIndex
}

func (t *MaxiTypeDef) IDFieldName() string {
	if f := t.IDField(); f != nil {
		return f.Name
	}
	return ""
}

func (t *MaxiTypeDef) IsRequired(i int) bool {
	t.ensureCache()
	if i >= 0 && i < len(t.requiredFlags) {
		return t.requiredFlags[i]
	}
	return false
}

func (t *MaxiTypeDef) HasRuntimeConstraints() bool {
	t.ensureCache()
	return t.hasRuntimeConstraints
}

type MaxiSchema struct {
	Version string `json:"version"`
	Imports []string `json:"imports,omitempty"`
	types     map[string]*MaxiTypeDef
	typeOrder []string
	nameIndex map[string]string
}

func NewMaxiSchema() *MaxiSchema {
	return &MaxiSchema{
		Version: "1.0.0",
		types:   make(map[string]*MaxiTypeDef),
	}
}

func (s *MaxiSchema) AddType(t *MaxiTypeDef) *MaxiError {
	if _, ok := s.types[t.Alias]; ok {
		return NewError(ErrDuplicateType, "duplicate type alias: "+t.Alias)
	}
	s.types[t.Alias] = t
	s.typeOrder = append(s.typeOrder, t.Alias)
	return nil
}

func (s *MaxiSchema) SetType(t *MaxiTypeDef) {
	if _, ok := s.types[t.Alias]; !ok {
		s.typeOrder = append(s.typeOrder, t.Alias)
	}
	s.types[t.Alias] = t
}

func (s *MaxiSchema) GetType(alias string) *MaxiTypeDef {
	return s.types[alias]
}

func (s *MaxiSchema) HasType(alias string) bool {
	_, ok := s.types[alias]
	return ok
}

func (s *MaxiSchema) Types() []*MaxiTypeDef {
	out := make([]*MaxiTypeDef, 0, len(s.typeOrder))
	for _, alias := range s.typeOrder {
		out = append(out, s.types[alias])
	}
	return out
}

func (s *MaxiSchema) BuildNameIndex() {
	idx := make(map[string]string, len(s.typeOrder))
	for _, alias := range s.typeOrder {
		td := s.types[alias]
		if td.Name != "" {
			if _, exists := idx[td.Name]; !exists {
				idx[td.Name] = alias
			}
		}
	}
	s.nameIndex = idx
}

func (s *MaxiSchema) ResolveTypeAlias(maybeAliasOrName string) string {
	if maybeAliasOrName == "" {
		return ""
	}
	if _, ok := s.types[maybeAliasOrName]; ok {
		return maybeAliasOrName
	}
	if s.nameIndex != nil {
		if alias, ok := s.nameIndex[maybeAliasOrName]; ok {
			return alias
		}
	}
	return ""
}

type MaxiRecord struct {
	Alias string `json:"alias"`
	Values []any `json:"values"`
	LineNumber int `json:"lineNumber,omitempty"`
}

type MaxiParseResult struct {
	Schema   *MaxiSchema    `json:"schema"`
	Records  []*MaxiRecord  `json:"records"`
	Warnings []*MaxiWarning `json:"warnings,omitempty"`
	ObjectRegistry any `json:"-"`
}

func NewMaxiParseResult() *MaxiParseResult {
	return &MaxiParseResult{
		Schema:  NewMaxiSchema(),
		Records: make([]*MaxiRecord, 0),
	}
}

func (r *MaxiParseResult) AddWarning(code, message string, line int) {
	r.Warnings = append(r.Warnings, &MaxiWarning{
		Code:    code,
		Message: message,
		Line:    line,
	})
}

type AdditionalFieldsMode string

const (
	AdditionalFieldsIgnore  AdditionalFieldsMode = "ignore"
	AdditionalFieldsWarning AdditionalFieldsMode = "warning"
	AdditionalFieldsError   AdditionalFieldsMode = "error"
)

type MissingFieldsMode string

const (
	MissingFieldsNull    MissingFieldsMode = "null"
	MissingFieldsWarning MissingFieldsMode = "warning"
	MissingFieldsError   MissingFieldsMode = "error"
)

type TypeCoercionMode string

const (
	TypeCoercionCoerce  TypeCoercionMode = "coerce"
	TypeCoercionWarning TypeCoercionMode = "warning"
	TypeCoercionError   TypeCoercionMode = "error"
)

type ConstraintViolationsMode string

const (
	ConstraintViolationsWarning ConstraintViolationsMode = "warning"
	ConstraintViolationsError   ConstraintViolationsMode = "error"
)

type UnknownTypesMode string

const (
	UnknownTypesIgnore  UnknownTypesMode = "ignore"
	UnknownTypesWarning UnknownTypesMode = "warning"
	UnknownTypesError   UnknownTypesMode = "error"
)

type ParseOptions struct {
	AllowAdditionalFields AdditionalFieldsMode
	AllowMissingFields MissingFieldsMode
	AllowTypeCoercion TypeCoercionMode
	AllowConstraintViolations ConstraintViolationsMode
	AllowForwardReferences bool
	AllowUnknownTypes UnknownTypesMode
	LoadSchema func(path string) (string, error)
}

func DefaultParseOptions() ParseOptions {
	return ParseOptions{
		AllowAdditionalFields:     AdditionalFieldsIgnore,
		AllowMissingFields:        MissingFieldsNull,
		AllowTypeCoercion:         TypeCoercionCoerce,
		AllowConstraintViolations: ConstraintViolationsWarning,
		AllowForwardReferences:    true,
		AllowUnknownTypes:         UnknownTypesWarning,
	}
}

type DumpOptions struct {
	DefaultAlias string
	Types []*MaxiTypeDef
	IncludeTypes bool
	SchemaFile string
	Version string
	Multiline bool
	CollectReferences bool
}

func DefaultDumpOptions() DumpOptions {
	return DumpOptions{
		IncludeTypes:      true,
		CollectReferences: true,
	}
}

