// Package api exposes the public MAXI parse, dump, hydrate, and stream API.
//
// The primary entry points are:
//
//   - [ParseMaxi] — parse MAXI text into a [core.MaxiParseResult]
//   - [DumpMaxi] — serialize objects or parse results back to MAXI text
//   - [DumpMaxiAuto] — serialize structs whose schema is described by maxi: struct tags
//   - [ParseMaxiAs] — parse + hydrate records into typed Go struct instances
//   - [StreamMaxi] — parse schema eagerly then yield records lazily via iter.Seq
//
// Full documentation: https://github.com/maxi-format/maxi-go/tree/main/docs
package api

import (
	"github.com/maxi-format/maxi-go/core"
	"github.com/maxi-format/maxi-go/internal"
)

type ObjectRegistry = internal.ObjectRegistry

func GetObjectRegistry(result *core.MaxiParseResult) ObjectRegistry {
	if result == nil || result.ObjectRegistry == nil {
		return nil
	}
	reg, _ := result.ObjectRegistry.(ObjectRegistry)
	return reg
}
