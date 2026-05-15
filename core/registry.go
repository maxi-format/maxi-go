package core

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
)

var explicitRegistry sync.Map

var tagCache sync.Map

// RegisterMaxiSchema associates a schema descriptor with a Go type.
//
// cls must be a non-nil pointer to a value of the target type
// (e.g. (*MyStruct)(nil)). Call this for types you don't own;
// for your own types prefer struct tags.
func RegisterMaxiSchema(cls any, schema *MaxiTypeDef) error {
	if cls == nil {
		return fmt.Errorf("RegisterMaxiSchema: cls must not be nil")
	}
	if schema == nil {
		return fmt.Errorf("RegisterMaxiSchema: schema must not be nil")
	}
	if schema.Alias == "" {
		return fmt.Errorf("RegisterMaxiSchema: schema.Alias is required")
	}
	t := reflect.TypeOf(cls)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	explicitRegistry.Store(t, schema)
	return nil
}

func UnregisterMaxiSchema(cls any) {
	t := reflect.TypeOf(cls)
	if t != nil && t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t != nil {
		explicitRegistry.Delete(t)
		tagCache.Delete(t)
	}
}

func LookupRegisteredSchema(cls any) *MaxiTypeDef {
	t := reflect.TypeOf(cls)
	if t == nil {
		return nil
	}
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if v, ok := explicitRegistry.Load(t); ok {
		return v.(*MaxiTypeDef)
	}
	return nil
}

// GetMaxiSchema returns the MAXI schema for a value or type pointer.
//
// Resolution order:
//  1. Struct tags (`maxi:"alias:U,name:User"` on the struct) — cached after
//     first reflection pass.
//  2. Explicit registry populated via RegisterMaxiSchema.
//
// Returns nil if no schema is found.
func GetMaxiSchema(v any) *MaxiTypeDef {
	if v == nil {
		return nil
	}

	t := reflect.TypeOf(v)
	if t == nil {
		return nil
	}
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}

	if cached, ok := tagCache.Load(t); ok {
		if cached == (*MaxiTypeDef)(nil) {
		} else if td, ok2 := cached.(*MaxiTypeDef); ok2 {
			return td
		}
	} else {
		td := deriveSchemaFromTags(t)
		if td != nil {
			tagCache.Store(t, td)
			return td
		}
		tagCache.Store(t, (*MaxiTypeDef)(nil))
	}

	if v2, ok := explicitRegistry.Load(t); ok {
		return v2.(*MaxiTypeDef)
	}

	return nil
}

// deriveSchemaFromTags inspects the struct type t for maxi tags and builds
// a *MaxiTypeDef if the type-level tag (alias:X) is present.
//
// Type-level tag: placed on a struct field named "_" or any embedded field:
//
//	type User struct {
//	    _ struct{} `maxi:"alias:U,name:User"`
//	    ID   int    `maxi:"id,type:int"`
//	    Name string `maxi:"name"`
//	}
//
// Alternatively, the alias/name can be embedded directly on the type by using
// the blank identifier field convention above, or on a field tagged
// `maxi:"type-meta"`.
//
// Field-level tags on exported struct fields:
//
//	`maxi:"fieldName"`                   – override wire name
//	`maxi:"fieldName,type:T"`            – with type expression
//	`maxi:"fieldName,type:T,ann:A"`      – with annotation
//	`maxi:"fieldName,id"`                – marks field as id
//	`maxi:"fieldName,required"`          – required constraint
//	`maxi:"-"`                           – skip this field
//
// The alias and name for the type can also be embedded at the type level by
// a field whose tag contains "alias:X" — regardless of field name.
func deriveSchemaFromTags(t reflect.Type) *MaxiTypeDef {
	alias := ""
	name := ""
	var fields []*MaxiFieldDef

	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		tag := sf.Tag.Get("maxi")
		if tag == "" {
			continue
		}
		if tag == "-" {
			continue
		}

		if extractTypeMeta(tag, &alias, &name) {
			continue
		}

		if !sf.IsExported() && sf.Name != "_" {
			continue
		}

		fd := parseFieldTag(sf, tag)
		if fd != nil {
			fields = append(fields, fd)
		}
	}

	if alias == "" {
		return nil
	}

	td := &MaxiTypeDef{
		Alias:  alias,
		Name:   name,
		Fields: fields,
	}
	return td
}

func extractTypeMeta(tag string, alias, name *string) bool {
	parts := splitTagParts(tag)
	hasAlias := false
	for _, p := range parts {
		if strings.HasPrefix(p, "alias:") {
			*alias = strings.TrimPrefix(p, "alias:")
			hasAlias = true
		} else if strings.HasPrefix(p, "name:") {
			*name = strings.TrimPrefix(p, "name:")
		}
	}
	return hasAlias
}

func parseFieldTag(sf reflect.StructField, tag string) *MaxiFieldDef {
	parts := splitTagParts(tag)
	if len(parts) == 0 {
		return nil
	}

	wireName := parts[0]
	directiveOnly := wireName == "" ||
		strings.HasPrefix(wireName, "alias:") ||
		strings.HasPrefix(wireName, "name:") ||
		strings.HasPrefix(wireName, "type:") ||
		strings.HasPrefix(wireName, "ann:") ||
		strings.HasPrefix(wireName, "default:")

	if directiveOnly {
		wireName = strings.ToLower(sf.Name)
		parts = append([]string{wireName}, parts...)
	}

	if wireName == "required" || wireName == "!" {
		wireName = strings.ToLower(sf.Name)
		parts = append([]string{wireName}, parts...)
	}

	fd := &MaxiFieldDef{Name: wireName}

	for _, p := range parts[1:] {
		switch {
		case p == "id":
			fd.Constraints = append(fd.Constraints, ParsedConstraint{Type: ConstraintID})
		case p == "required" || p == "!":
			fd.Constraints = append(fd.Constraints, ParsedConstraint{Type: ConstraintRequired})
		case strings.HasPrefix(p, "type:"):
			fd.TypeExpr = strings.TrimPrefix(p, "type:")
		case strings.HasPrefix(p, "ann:"):
			fd.Annotation = strings.TrimPrefix(p, "ann:")
		case strings.HasPrefix(p, "default:"):
			fd.DefaultValue = strings.TrimPrefix(p, "default:")
		}
	}

	return fd
}

func splitTagParts(tag string) []string {
	var parts []string
	for _, s := range strings.Split(tag, ",") {
		parts = append(parts, strings.TrimSpace(s))
	}
	return parts
}

