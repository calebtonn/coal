package manifest

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
	"golang.org/x/text/language"
	"golang.org/x/text/message"

	"github.com/calebtonn/coal/spec"
)

var compiled = sync.OnceValues(func() (*jsonschema.Schema, error) {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(spec.IntentManifestSchema))
	if err != nil {
		return nil, fmt.Errorf("parse embedded schema: %w", err)
	}
	c := jsonschema.NewCompiler()
	c.AssertFormat()
	if err := c.AddResource(SchemaURL, doc); err != nil {
		return nil, fmt.Errorf("load embedded schema: %w", err)
	}
	s, err := c.Compile(SchemaURL)
	if err != nil {
		return nil, fmt.Errorf("compile embedded schema: %w", err)
	}
	return s, nil
})

// Validate checks data against the Intent Manifest schema. The error
// has one line per problem, each starting with the JSON pointer of the
// offending value.
func Validate(data []byte) error {
	s, err := compiled()
	if err != nil {
		return err
	}
	v, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("not valid JSON: %w", err)
	}
	err = s.Validate(v)
	var ve *jsonschema.ValidationError
	if !errors.As(err, &ve) {
		return err
	}
	p := message.NewPrinter(language.English)
	var lines []string
	collect(ve, p, &lines)
	sort.Strings(lines)
	return errors.New(strings.Join(lines, "\n"))
}

// collect appends one line per leaf error.
func collect(e *jsonschema.ValidationError, p *message.Printer, lines *[]string) {
	if len(e.Causes) == 0 {
		loc := "/" + strings.Join(e.InstanceLocation, "/")
		msg := e.ErrorKind.LocalizedString(p)
		if _, ok := e.ErrorKind.(*kind.Not); ok {
			msg = "matches a disallowed pattern (" + e.SchemaURL + ")"
			if strings.HasSuffix(loc, "/sessionRef") {
				msg = "looks like an auth token; sessionRef must be an opaque locator"
			}
		}
		*lines = append(*lines, loc+": "+msg)
		return
	}
	for _, c := range e.Causes {
		collect(c, p, lines)
	}
}
