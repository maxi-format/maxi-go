package api

import (
	"regexp"
	"strings"

	"github.com/maxi-format/maxi-go/core"
	"github.com/maxi-format/maxi-go/internal"
)

var separatorRe = regexp.MustCompile(`(?m)^[ \t]*###[ \t]*(?:\r?\n|$)`)

var hasExplicitTypeDefRe = regexp.MustCompile(`(?m)^[ \t]*[A-Za-z_][A-Za-z0-9_-]*[ \t]*:`)

var hasInheritanceTypeDefRe = regexp.MustCompile(`(?m)^[ \t]*[A-Za-z_][A-Za-z0-9_-]*[ \t]*<[^>]+>[ \t]*\(`)

var hasDirectiveRe = regexp.MustCompile(`(?m)^[ \t]*@`)

// ParseMaxi parses a MAXI-formatted string and returns a MaxiParseResult.
//
// The opts argument, if provided, overrides the spec-default ParseOptions.
// Pass a zero ParseOptions{} to use fully custom options; pass
// core.DefaultParseOptions() to start from defaults and selectively override.
func ParseMaxi(input string, opts ...core.ParseOptions) (*core.MaxiParseResult, error) {
	options := core.DefaultParseOptions()
	if len(opts) > 0 {
		options = opts[0]
	}

	result := core.NewMaxiParseResult()

	schemaSection, recordsSection := splitSections(input)

	sp := internal.NewSchemaParser(schemaSection, result, options, "")
	if err := sp.Parse(); err != nil {
		return nil, err
	}

	if recordsSection != "" {
		rp := internal.NewRecordParser(recordsSection, result, options, "")
		if err := rp.Parse(); err != nil {
			return nil, err
		}
	}

	if len(result.Records) > 0 {
		reg := internal.BuildObjectRegistry(result)
		result.ObjectRegistry = reg
		if len(result.Schema.Types()) > 0 && internal.HasReferenceFields(result.Schema) {
			if err := internal.ValidateReferences(result, reg, options); err != nil {
				return nil, err
			}
		}
	}

	return result, nil
}

func splitSections(input string) (schema, records string) {
	loc := separatorRe.FindStringIndex(input)
	if loc != nil {
		schema = strings.TrimSpace(input[:loc[0]])
		records = strings.TrimSpace(input[loc[1]:])
		return
	}

	hasDirective := hasDirectiveRe.MatchString(input)
	hasExplicit := hasExplicitTypeDefRe.MatchString(input)
	hasInherit := hasInheritanceTypeDefRe.MatchString(input)

	if hasDirective || hasExplicit || hasInherit {
		if hasDirective && !hasExplicit && !hasInherit {
			lines := strings.Split(strings.ReplaceAll(input, "\r\n", "\n"), "\n")
			var schemaLines, recordLines []string
			for _, line := range lines {
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, "@") || trimmed == "" || strings.HasPrefix(trimmed, "#") {
					schemaLines = append(schemaLines, line)
				} else {
					recordLines = append(recordLines, line)
				}
			}
			schema = strings.TrimSpace(strings.Join(schemaLines, "\n"))
			records = strings.TrimSpace(strings.Join(recordLines, "\n"))
			return
		}
		schema = input
		records = ""
		return
	}

	schema = ""
	records = input
	return
}
