package api

import (
	"iter"
	"strings"

	"github.com/maxi-format/maxi-go/core"
	"github.com/maxi-format/maxi-go/internal"
)

// MaxiStreamResult is returned by StreamMaxi.
// Schema and Warnings are available immediately (schema phase is complete).
// Records are yielded lazily via the Records() iterator.
type MaxiStreamResult struct {
	Schema *core.MaxiSchema
	result      *core.MaxiParseResult
	recordsText string
	parser      *internal.RecordParser
}

func (s *MaxiStreamResult) Warnings() []*core.MaxiWarning {
	if s.result.Warnings == nil {
		return []*core.MaxiWarning{}
	}
	return s.result.Warnings
}

// Records returns an iter.Seq[*core.MaxiRecord] that yields parsed records
// one at a time.  The iterator is safe to range over with Go 1.23+:
//
//	for rec := range stream.Records() { ... }
//
// Errors during record parsing are surfaced as panics when iterating.
// To handle errors explicitly, use RecordsWithError instead.
func (s *MaxiStreamResult) Records() iter.Seq[*core.MaxiRecord] {
	return func(yield func(*core.MaxiRecord) bool) {
		scanRecords(s.recordsText, s.parser, func(rec *core.MaxiRecord, err error) bool {
			if err != nil {
				panic(err)
			}
			return yield(rec)
		})
	}
}

// RecordsWithError returns an iter.Seq2[*core.MaxiRecord, error] that yields
// each record together with any parse error.  Iteration stops when the yield
// function returns false or when the input is exhausted.
//
//	for rec, err := range stream.RecordsWithError() {
//	    if err != nil { ... }
//	}
func (s *MaxiStreamResult) RecordsWithError() iter.Seq2[*core.MaxiRecord, error] {
	return func(yield func(*core.MaxiRecord, error) bool) {
		scanRecords(s.recordsText, s.parser, func(rec *core.MaxiRecord, err error) bool {
			return yield(rec, err)
		})
	}
}

// StreamMaxi parses the schema section eagerly and returns a MaxiStreamResult
// whose Records() iterator yields records lazily one at a time.
//
// Phase 1 (schema parsing) completes before StreamMaxi returns; any schema
// errors are returned immediately.  Record errors surface during iteration.
func StreamMaxi(input string, opts ...core.ParseOptions) (*MaxiStreamResult, error) {
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

	rp := internal.NewRecordParser("", result, options, "")

	sr := &MaxiStreamResult{
		Schema:      result.Schema,
		result:      result,
		recordsText: recordsSection,
		parser:      rp,
	}
	return sr, nil
}

func scanRecords(text string, parser *internal.RecordParser, cb func(*core.MaxiRecord, error) bool) {
	if strings.TrimSpace(text) == "" {
		return
	}

	n := len(text)
	i := 0
	lineNumber := 1

	isIdentStart := func(c byte) bool {
		return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '_'
	}
	isIdentChar := func(c byte) bool {
		return isIdentStart(c) || (c >= '0' && c <= '9') || c == '-'
	}

	for i < n {
		ch := text[i]
		switch {
		case ch == '\n':
			lineNumber++
			i++
			continue
		case ch == ' ' || ch == '\t' || ch == '\r':
			i++
			continue
		case !isIdentStart(ch):
			i++
			continue
		}

		aliasStart := i
		i++
		for i < n && isIdentChar(text[i]) {
			i++
		}
		alias := text[aliasStart:i]

		for i < n && (text[i] == ' ' || text[i] == '\t' || text[i] == '\r') {
			i++
		}
		if i >= n || text[i] != '(' {
			continue
		}

		recordLine := lineNumber
		i++
		valuesStart := i

		parenDepth := 1
		bracketDepth := 0
		braceDepth := 0
		inString := false
		escapeNext := false

		for i < n {
			c := text[i]
			if c == '\n' {
				lineNumber++
			}
			if escapeNext {
				escapeNext = false
				i++
				continue
			}
			if inString {
				if c == '\\' {
					escapeNext = true
				} else if c == '"' {
					inString = false
				}
				i++
				continue
			}
			switch c {
			case '"':
				inString = true
			case '(':
				parenDepth++
			case ')':
				parenDepth--
				if parenDepth == 0 {
					goto endRecord
				}
			case '[':
				bracketDepth++
			case ']':
				if bracketDepth > 0 {
					bracketDepth--
				}
			case '{':
				braceDepth++
			case '}':
				if braceDepth > 0 {
					braceDepth--
				}
			}
			i++
		}

	endRecord:
		if i >= n || text[i] != ')' || parenDepth != 0 {
			err := core.NewErrorAt(core.ErrInvalidSyntax,
				"unclosed record parentheses for '"+alias+"'", recordLine, 0)
			if !cb(nil, err) {
				return
			}
			continue
		}

		valuesStr := text[valuesStart:i]
		i++

		rec, err := parser.ParseSingleRecord(alias, valuesStr, recordLine)
		if !cb(rec, err) {
			return
		}
	}
}
