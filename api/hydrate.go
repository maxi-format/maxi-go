package api

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/maxi-format/maxi-go/core"
)

type MaxiHydrateResult struct {
	Objects  map[string][]any
	Schema   *core.MaxiSchema
	Warnings []*core.MaxiWarning
}

// ParseMaxiAs parses a MAXI string and hydrates each record into an instance
// of the corresponding Go type in classMap.
//
// classMap maps alias → reflect.Type of the target struct type
// (e.g. reflect.TypeOf(User{})).
//
// After parsing, cross-reference fields (whose typeExpr is another registered
// alias) are resolved from scalar IDs to the actual hydrated instance.
func ParseMaxiAs(input string, classMap map[string]reflect.Type, opts ...core.ParseOptions) (*MaxiHydrateResult, error) {
	if classMap == nil {
		return nil, fmt.Errorf("ParseMaxiAs: classMap must not be nil")
	}

	result, err := ParseMaxi(input, opts...)
	if err != nil {
		return nil, err
	}
	return hydrateResult(result, classMap)
}

// ParseMaxiAutoAs is a generic convenience wrapper around ParseMaxiAs.
// It builds the classMap automatically by calling core.GetMaxiSchema on a
// zero value of T to find T's alias, then includes any additional types
// passed in extraTypes.
//
// Example:
//
//	res, err := ParseMaxiAutoAs[User](input)
//	res, err := ParseMaxiAutoAs[User](input, reflect.TypeOf(Order{}))
func ParseMaxiAutoAs[T any](input string, opts ...core.ParseOptions) (*MaxiHydrateResult, error) {
	var zero T
	schema := core.GetMaxiSchema(zero)
	if schema == nil {
		// Try pointer
		schema = core.GetMaxiSchema(&zero)
	}
	if schema == nil {
		return nil, fmt.Errorf(
			"ParseMaxiAutoAs: no schema found for type %T. "+
				"Attach maxi struct tags or use RegisterMaxiSchema",
			zero,
		)
	}
	t := reflect.TypeOf(zero)
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	classMap := map[string]reflect.Type{schema.Alias: t}
	return ParseMaxiAs(input, classMap, opts...)
}

// ParseMaxiAutoAsMulti parses a MAXI string and hydrates records into
// instances of the supplied types. Each type must have a registered schema
// (struct tags or RegisterMaxiSchema).
//
// This is the non-generic equivalent of parseMaxiAutoAs from the JS/Python
// implementations — pass a slice of reflect.Type values.
func ParseMaxiAutoAsMulti(input string, types []reflect.Type, opts ...core.ParseOptions) (*MaxiHydrateResult, error) {
	classMap := make(map[string]reflect.Type, len(types))
	for _, t := range types {
		for t.Kind() == reflect.Ptr {
			t = t.Elem()
		}
		// Create a zero value to look up the schema
		zv := reflect.New(t).Elem().Interface()
		schema := core.GetMaxiSchema(zv)
		if schema == nil {
			return nil, fmt.Errorf(
				"ParseMaxiAutoAsMulti: no schema found for type %s. "+
					"Attach maxi struct tags or use RegisterMaxiSchema",
				t.Name(),
			)
		}
		classMap[schema.Alias] = t
	}
	return ParseMaxiAs(input, classMap, opts...)
}

func hydrateResult(result *core.MaxiParseResult, classMap map[string]reflect.Type) (*MaxiHydrateResult, error) {
	schemaByAlias := make(map[string]*core.MaxiTypeDef, len(classMap))
	for alias := range classMap {
		if td := result.Schema.GetType(alias); td != nil {
			schemaByAlias[alias] = td
		} else {
			t := classMap[alias]
			zv := reflect.New(t).Elem().Interface()
			if td2 := core.GetMaxiSchema(zv); td2 != nil {
				schemaByAlias[alias] = td2
			}
		}
	}

	objects := make(map[string][]any)
	instanceRegistry := make(map[string]map[string]any)
	instancesByAlias := make(map[string][]instanceEntry)

	for _, rec := range result.Records {
		targetType, ok := classMap[rec.Alias]
		if !ok {
			continue
		}
		td := schemaByAlias[rec.Alias]
		fieldMap := recordToFieldMap(rec, td)

		instance, err := constructInstance(targetType, fieldMap)
		if err != nil {
			return nil, fmt.Errorf("hydrate %s: %w", rec.Alias, err)
		}

		objects[rec.Alias] = append(objects[rec.Alias], instance)
		instancesByAlias[rec.Alias] = append(instancesByAlias[rec.Alias], instanceEntry{instance, fieldMap})

		if td != nil {
			idFieldName := findIDFieldName(td)
			if idFieldName != "" {
				if idVal := fieldMap[idFieldName]; idVal != nil {
					idStr := fmt.Sprintf("%v", idVal)
					if instanceRegistry[rec.Alias] == nil {
						instanceRegistry[rec.Alias] = make(map[string]any)
					}
					instanceRegistry[rec.Alias][idStr] = instance
				}
			}
		}
	}

	resolveReferences(instancesByAlias, schemaByAlias, instanceRegistry, result.Schema)

	warnings := result.Warnings
	if warnings == nil {
		warnings = []*core.MaxiWarning{}
	}
	return &MaxiHydrateResult{
		Objects:  objects,
		Schema:   result.Schema,
		Warnings: warnings,
	}, nil
}

func recordToFieldMap(rec *core.MaxiRecord, td *core.MaxiTypeDef) map[string]any {
	m := make(map[string]any)
	if td != nil && len(td.Fields) > 0 {
		for i, f := range td.Fields {
			if i < len(rec.Values) {
				m[f.Name] = rec.Values[i]
			} else {
				if f.DefaultValue != nil {
					m[f.Name] = f.DefaultValue
				} else {
					m[f.Name] = nil
				}
			}
		}
	} else {
		for i, v := range rec.Values {
			m[fmt.Sprintf("%d", i)] = v
		}
	}
	return m
}

func constructInstance(targetType reflect.Type, fieldMap map[string]any) (any, error) {
	for targetType.Kind() == reflect.Ptr {
		targetType = targetType.Elem()
	}
	pv := reflect.New(targetType)
	v := pv.Elem()

	nameToIdx := buildNameIndex(targetType)

	for wireName, val := range fieldMap {
		idx, ok := nameToIdx[wireName]
		if !ok {
			idx, ok = nameToIdx[strings.ToLower(wireName)]
		}
		if !ok {
			continue
		}
		sf := targetType.Field(idx)
		fv := v.Field(idx)
		if err := setField(fv, sf.Type, val); err != nil {
			continue
		}
	}

	return pv.Interface(), nil
}

func buildNameIndex(t reflect.Type) map[string]int {
	m := make(map[string]int, t.NumField()*2)
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if !sf.IsExported() {
			continue
		}
		m[sf.Name] = i
		m[strings.ToLower(sf.Name)] = i

		if tag := sf.Tag.Get("maxi"); tag != "" && tag != "-" {
			parts := strings.Split(tag, ",")
			if len(parts) > 0 {
				wireName := strings.TrimSpace(parts[0])
				if wireName != "" &&
					!strings.HasPrefix(wireName, "alias:") &&
					!strings.HasPrefix(wireName, "name:") &&
					!strings.HasPrefix(wireName, "type:") &&
					!strings.HasPrefix(wireName, "ann:") &&
					!strings.HasPrefix(wireName, "default:") {
					m[wireName] = i
				}
			}
		}
	}
	return m
}

func setField(fv reflect.Value, ft reflect.Type, val any) error {
	if val == nil {
		fv.Set(reflect.Zero(ft))
		return nil
	}

	rv := reflect.ValueOf(val)

	if ft.Kind() == reflect.Ptr {
		pv := reflect.New(ft.Elem())
		if err := setField(pv.Elem(), ft.Elem(), val); err != nil {
			return err
		}
		fv.Set(pv)
		return nil
	}

	if rv.Type().AssignableTo(ft) {
		fv.Set(rv)
		return nil
	}

	if rv.Type().ConvertibleTo(ft) {
		fv.Set(rv.Convert(ft))
		return nil
	}

	if ft.Kind() == reflect.String {
		fv.SetString(fmt.Sprintf("%v", val))
		return nil
	}

	if ft.Kind() >= reflect.Int && ft.Kind() <= reflect.Int64 {
		if s, ok := val.(string); ok {
			var n int64
			if _, err := fmt.Sscan(s, &n); err == nil {
				fv.SetInt(n)
				return nil
			}
		}
	}
	if ft.Kind() >= reflect.Uint && ft.Kind() <= reflect.Uint64 {
		if s, ok := val.(string); ok {
			var n uint64
			if _, err := fmt.Sscan(s, &n); err == nil {
				fv.SetUint(n)
				return nil
			}
		}
	}
	if ft.Kind() == reflect.Float32 || ft.Kind() == reflect.Float64 {
		if s, ok := val.(string); ok {
			var f float64
			if _, err := fmt.Sscan(s, &f); err == nil {
				fv.SetFloat(f)
				return nil
			}
		}
	}
	if ft.Kind() == reflect.Bool {
		switch v := val.(type) {
		case bool:
			fv.SetBool(v)
			return nil
		case int64:
			fv.SetBool(v != 0)
			return nil
		case float64:
			fv.SetBool(v != 0)
			return nil
		}
	}

	return fmt.Errorf("cannot set field of type %s from %T", ft, val)
}

func findIDFieldName(td *core.MaxiTypeDef) string {
	if td == nil {
		return ""
	}
	for _, f := range td.Fields {
		if f.IsID() {
			return f.Name
		}
	}
	for _, f := range td.Fields {
		if f.Name == "id" {
			return f.Name
		}
	}
	return ""
}

var nonRefTypes = map[string]bool{
	"str": true, "int": true, "decimal": true,
	"float": true, "bool": true, "bytes": true,
}

type instanceEntry struct {
	instance any
	fieldMap map[string]any
}

func resolveReferences(
	instancesByAlias map[string][]instanceEntry,
	schemaByAlias map[string]*core.MaxiTypeDef,
	instanceRegistry map[string]map[string]any,
	parsedSchema *core.MaxiSchema,
) {
	for alias, entries := range instancesByAlias {
		td := schemaByAlias[alias]
		if td == nil {
			continue
		}
		for _, entry := range entries {
			rv := reflect.ValueOf(entry.instance)
			for rv.Kind() == reflect.Ptr {
				rv = rv.Elem()
			}
			if rv.Kind() != reflect.Struct {
				continue
			}
			nameIdx := buildNameIndex(rv.Type())

			for _, field := range td.Fields {
				refAlias := getRefAlias(field.TypeExpr, parsedSchema)
				if refAlias == "" {
					continue
				}
				refReg := instanceRegistry[refAlias]
				if refReg == nil {
					continue
				}

				rawID := entry.fieldMap[field.Name]
				if rawID == nil {
					continue
				}
				rawRv := reflect.ValueOf(rawID)
				if rawRv.Kind() == reflect.Struct || rawRv.Kind() == reflect.Map {
					continue
				}
				if rawRv.Kind() == reflect.Ptr && !rawRv.IsNil() {
					continue
				}

				idStr := fmt.Sprintf("%v", rawID)
				resolved, found := refReg[idStr]
				if !found {
					continue
				}

				idx, ok := nameIdx[field.Name]
				if !ok {
					continue
				}
				fv := rv.Field(idx)
				if !fv.CanSet() {
					continue
				}

				resolvedRv := reflect.ValueOf(resolved)
				ft := fv.Type()

				switch ft.Kind() {
				case reflect.Ptr:
					if resolvedRv.Type().AssignableTo(ft) {
						fv.Set(resolvedRv)
					} else if resolvedRv.Kind() == reflect.Ptr &&
						resolvedRv.Elem().Type().AssignableTo(ft.Elem()) {
						fv.Set(resolvedRv)
					}
				case reflect.Interface:
					fv.Set(resolvedRv)
				default:
					if resolvedRv.Type().AssignableTo(ft) {
						fv.Set(resolvedRv)
					} else if resolvedRv.Kind() == reflect.Ptr &&
						resolvedRv.Elem().Type().AssignableTo(ft) {
						fv.Set(resolvedRv.Elem())
					}
				}
			}
		}
	}
}

func getRefAlias(typeExpr string, schema *core.MaxiSchema) string {
	if typeExpr == "" {
		return ""
	}
	t := strings.TrimSpace(typeExpr)
	for strings.HasSuffix(t, "[]") {
		t = t[:len(t)-2]
	}
	if nonRefTypes[t] {
		return ""
	}
	if t == "map" || strings.HasPrefix(t, "map<") {
		return ""
	}
	if strings.HasPrefix(t, "enum") {
		return ""
	}
	if schema.HasType(t) {
		return t
	}
	return ""
}
