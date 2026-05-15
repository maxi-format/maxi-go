package api

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/maxi-format/maxi-go/core"
)

// DumpMaxiAuto serialises class instances (structs) or plain maps into a MAXI
// string, inferring schemas automatically from struct tags or the explicit
// registry (core.RegisterMaxiSchema / core.GetMaxiSchema).
//
// objects may be:
//   - []any or []SomeStruct — all items share the same alias (requires the
//     first element to have a registered schema, or opts.DefaultAlias to be set)
//   - map[string][]any      — alias → rows map (each row may be a struct or map)
//
// Any opts.Types supplied are merged with the auto-collected schemas; caller
// wins on conflict. All other DumpOptions are forwarded unchanged to DumpMaxi.
func DumpMaxiAuto(objects any, opts ...DumpOptions) (string, error) {
	o := DefaultDumpOptions()
	if len(opts) > 0 {
		o = opts[0]
	}

	dataMap, err := autoDumpNormalizeInput(objects, o.DefaultAlias)
	if err != nil {
		return "", err
	}

	collected := make(map[string]*core.MaxiTypeDef)
	for _, rows := range dataMap {
		for _, obj := range rows {
			if obj != nil {
				collectSchemasDeep(obj, collected)
			}
		}
	}

	for _, td := range o.Types {
		if td != nil {
			collected[td.Alias] = td
		}
	}

	callerAliases := make(map[string]bool, len(o.Types))
	mergedTypes := make([]*core.MaxiTypeDef, 0, len(collected))
	for _, td := range o.Types {
		if td != nil {
			mergedTypes = append(mergedTypes, td)
			callerAliases[td.Alias] = true
		}
	}
	for _, td := range collected {
		if !callerAliases[td.Alias] {
			mergedTypes = append(mergedTypes, td)
		}
	}

	flatMap := make(map[string][]map[string]any, len(dataMap))
	for alias, rows := range dataMap {
		td := collected[alias]
		flat := make([]map[string]any, 0, len(rows))
		for _, obj := range rows {
			m, convErr := toFieldMap(obj, td)
			if convErr != nil {
				return "", fmt.Errorf("DumpMaxiAuto: alias %q: %w", alias, convErr)
			}
			flat = append(flat, m)
		}
		flatMap[alias] = flat
	}

	dumpOpts := o
	dumpOpts.Types = mergedTypes
	return DumpMaxi(flatMap, dumpOpts)
}

func autoDumpNormalizeInput(objects any, defaultAlias string) (map[string][]any, error) {
	if objects == nil {
		return nil, fmt.Errorf("DumpMaxiAuto: objects must not be nil")
	}

	rv := reflect.ValueOf(objects)
	switch rv.Kind() {
	case reflect.Slice:
		alias := defaultAlias
		if rv.Len() > 0 {
			first := rv.Index(0).Interface()
			if schema := core.GetMaxiSchema(first); schema != nil {
				alias = schema.Alias
			}
		} else if alias == "" {
			if schema := core.GetMaxiSchema(reflect.New(rv.Type().Elem()).Interface()); schema != nil {
				alias = schema.Alias
			}
		}
		if alias == "" {
			return nil, fmt.Errorf(
				"DumpMaxiAuto: cannot determine alias for the array. " +
					"Attach maxi struct tags or RegisterMaxiSchema, or pass DefaultAlias in options",
			)
		}
		items := make([]any, rv.Len())
		for i := range items {
			items[i] = rv.Index(i).Interface()
		}
		return map[string][]any{alias: items}, nil

	case reflect.Map:
		out := make(map[string][]any)
		for _, k := range rv.MapKeys() {
			alias := fmt.Sprintf("%v", k.Interface())
			vv := rv.MapIndex(k)
			for vv.Kind() == reflect.Interface {
				vv = vv.Elem()
			}
			if vv.Kind() != reflect.Slice {
				return nil, fmt.Errorf("DumpMaxiAuto: value for key %q must be a slice", alias)
			}
			rows := make([]any, vv.Len())
			for i := range rows {
				rows[i] = vv.Index(i).Interface()
			}
			out[alias] = rows
		}
		return out, nil

	default:
		return nil, fmt.Errorf("DumpMaxiAuto: objects must be a slice or map, got %T", objects)
	}
}

func collectSchemasDeep(obj any, collected map[string]*core.MaxiTypeDef) {
	if obj == nil {
		return
	}
	schema := core.GetMaxiSchema(obj)
	if schema == nil {
		return
	}
	if _, already := collected[schema.Alias]; already {
		return
	}
	collected[schema.Alias] = schema

	rv := reflect.ValueOf(obj)
	for rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return
		}
		rv = rv.Elem()
	}

	switch rv.Kind() {
	case reflect.Struct:
		rt := rv.Type()
		for i := 0; i < rv.NumField(); i++ {
			sf := rt.Field(i)
			if !sf.IsExported() {
				continue
			}
			fv := rv.Field(i)
			recurseIntoValue(fv, collected)
		}
	case reflect.Map:
		for _, k := range rv.MapKeys() {
			recurseIntoValue(rv.MapIndex(k), collected)
		}
	}
}

func recurseIntoValue(fv reflect.Value, collected map[string]*core.MaxiTypeDef) {
	for fv.Kind() == reflect.Interface || fv.Kind() == reflect.Ptr {
		if fv.IsNil() {
			return
		}
		fv = fv.Elem()
	}
	switch fv.Kind() {
	case reflect.Struct:
		collectSchemasDeep(fv.Interface(), collected)
	case reflect.Slice:
		for i := 0; i < fv.Len(); i++ {
			recurseIntoValue(fv.Index(i), collected)
		}
	}
}

func toFieldMap(obj any, td *core.MaxiTypeDef) (map[string]any, error) {
	if obj == nil {
		return map[string]any{}, nil
	}

	if m, ok := obj.(map[string]any); ok {
		return m, nil
	}

	rv := reflect.ValueOf(obj)
	for rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return map[string]any{}, nil
		}
		rv = rv.Elem()
	}

	if rv.Kind() != reflect.Struct {
		return nil, fmt.Errorf("expected struct or map[string]any, got %T", obj)
	}

	if td != nil {
		return structToMapWithSchema(rv, td)
	}
	return structToMapReflect(rv), nil
}

func structToMapWithSchema(rv reflect.Value, td *core.MaxiTypeDef) (map[string]any, error) {
	rt := rv.Type()

	nameToIdx := make(map[string]int, rt.NumField()*2)
	for i := 0; i < rt.NumField(); i++ {
		sf := rt.Field(i)
		if !sf.IsExported() {
			continue
		}
		lc := strings.ToLower(sf.Name)
		nameToIdx[lc] = i
		nameToIdx[sf.Name] = i

		if tag := sf.Tag.Get("maxi"); tag != "" && tag != "-" {
			parts := splitTagPartsAuto(tag)
			if len(parts) > 0 && parts[0] != "" &&
				!hasPrefix(parts[0], "alias:") && !hasPrefix(parts[0], "name:") &&
				parts[0] != "id" && parts[0] != "required" && parts[0] != "!" &&
				!hasPrefix(parts[0], "type:") && !hasPrefix(parts[0], "ann:") &&
				!hasPrefix(parts[0], "default:") {
				nameToIdx[parts[0]] = i
			}
		}
	}

	out := make(map[string]any, len(td.Fields))
	for _, f := range td.Fields {
		idx, ok := nameToIdx[f.Name]
		if !ok {
			fl := strings.ToLower(f.Name)
			idx, ok = nameToIdx[fl]
		}
		if !ok {
			continue
		}
		fv := rv.Field(idx)
		out[f.Name] = exportValue(fv)
	}
	return out, nil
}

func structToMapReflect(rv reflect.Value) map[string]any {
	rt := rv.Type()
	out := make(map[string]any, rt.NumField())
	for i := 0; i < rt.NumField(); i++ {
		sf := rt.Field(i)
		if !sf.IsExported() {
			continue
		}
		out[strings.ToLower(sf.Name)] = exportValue(rv.Field(i))
	}
	return out
}

func exportValue(fv reflect.Value) any {
	for fv.Kind() == reflect.Ptr || fv.Kind() == reflect.Interface {
		if fv.IsNil() {
			return nil
		}
		fv = fv.Elem()
	}

	switch fv.Kind() {
	case reflect.Struct:
		td := core.GetMaxiSchema(fv.Interface())
		if td != nil {
			m, _ := structToMapWithSchema(fv, td)
			return m
		}
		return structToMapReflect(fv)

	case reflect.Slice:
		if fv.IsNil() {
			return nil
		}
		if fv.Type().Elem().Kind() == reflect.Uint8 {
			return fv.Bytes()
		}
		out := make([]any, fv.Len())
		for i := range out {
			out[i] = exportValue(fv.Index(i))
		}
		return out

	case reflect.Map:
		out := make(map[string]any, fv.Len())
		for _, k := range fv.MapKeys() {
			out[fmt.Sprintf("%v", k.Interface())] = exportValue(fv.MapIndex(k))
		}
		return out

	default:
		if fv.CanInterface() {
			return fv.Interface()
		}
		return nil
	}
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func splitTagPartsAuto(tag string) []string {
	var parts []string
	for _, s := range splitOnComma(tag) {
		parts = append(parts, trimSpace(s))
	}
	return parts
}

func splitOnComma(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}
