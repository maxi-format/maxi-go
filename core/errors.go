package core

import "fmt"

const (
	// E1xx — Schema definition errors
	ErrInvalidSyntax           = "E101"
	ErrDuplicateType           = "E102"
	ErrUnknownDirective        = "E103"
	// E2xx — Type system errors
	ErrUnknownType             = "E201"
	ErrUndefinedParent         = "E202"
	ErrCircularInheritance     = "E203"
	ErrUnresolvedReference     = "E204"
	ErrDuplicateIdentifier     = "E205"
	// E3xx — Constraint errors
	ErrConstraintSyntax        = "E301"
	ErrInvalidConstraintValue  = "E302"
	ErrConstraintViolation     = "E303"
	ErrArraySyntax             = "E304"
	// E4xx — Data record errors
	ErrSchemaMismatch          = "E401"
	ErrTypeMismatch            = "E402"
	ErrMissingRequiredField    = "E403"
	ErrInvalidDefaultValue     = "E404"
	ErrUnsupportedBinaryFormat = "E405"
	// E5xx — Data type errors
	ErrEnumAliasError          = "E501"
	// E6xx — IO / runtime errors
	ErrUnsupportedVersion      = "E601"
	ErrSchemaLoad              = "E602"
	ErrStream                  = "E603"
)

type MaxiError struct {
	Message string
	Code string
	Line int
	Column int
	Filename string
	Cause error
}

func (e *MaxiError) Error() string {
	loc := ""
	if e.Line > 0 {
		loc = fmt.Sprintf(" at line %d", e.Line)
		if e.Column > 0 {
			loc += fmt.Sprintf(", column %d", e.Column)
		}
	}
	file := ""
	if e.Filename != "" {
		file = " in " + e.Filename
	}
	return fmt.Sprintf("MaxiError [%s]%s%s: %s", e.Code, file, loc, e.Message)
}

func (e *MaxiError) Unwrap() error { return e.Cause }

func NewError(code, message string) *MaxiError {
	return &MaxiError{Code: code, Message: message}
}

func NewErrorAt(code, message string, line, column int) *MaxiError {
	return &MaxiError{Code: code, Message: message, Line: line, Column: column}
}

type MaxiWarning struct {
	Message string
	Code    string
	Line    int
	Column  int
}

func (w *MaxiWarning) String() string {
	loc := ""
	if w.Line > 0 {
		loc = fmt.Sprintf(" at line %d", w.Line)
	}
	return fmt.Sprintf("MaxiWarning [%s]%s: %s", w.Code, loc, w.Message)
}
