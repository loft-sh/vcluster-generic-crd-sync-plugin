package yaml

import (
	"bytes"
	"fmt"
	"gopkg.in/yaml.v3"
	"regexp"
	"strconv"
	"strings"
)

func UnmarshalStrict(data []byte, out interface{}) error {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	err := decoder.Decode(out)
	return prettifyError(data, err)
}

var lineRegEx = regexp.MustCompile(`^line ([0-9]+):`)

func prettifyError(data []byte, err error) error {
	// check if type error
	if typeError, ok := err.(*yaml.TypeError); ok {
		// print the config with lines
		lines := strings.Split(string(data), "\n")
		extraLines := []string{"Parsed Config:"}
		for i, v := range lines {
			if v == "" {
				continue
			}
			extraLines = append(extraLines, fmt.Sprintf("  %d: %s", i+1, v))
		}
		extraLines = append(extraLines, "Errors:")

		for i := range typeError.Errors {
			typeError.Errors[i] = strings.ReplaceAll(typeError.Errors[i], "!!seq", "an array")
			typeError.Errors[i] = strings.ReplaceAll(typeError.Errors[i], "!!str", "string")
			typeError.Errors[i] = strings.ReplaceAll(typeError.Errors[i], "!!map", "an object")
			typeError.Errors[i] = strings.ReplaceAll(typeError.Errors[i], "!!int", "number")
			typeError.Errors[i] = strings.ReplaceAll(typeError.Errors[i], "!!bool", "boolean")

			// add line to error
			match := lineRegEx.FindSubmatch([]byte(typeError.Errors[i]))
			if len(match) > 1 {
				line, lineErr := strconv.Atoi(string(match[1]))
				if lineErr == nil {
					line = line - 1
					lines := strings.Split(string(data), "\n")
					if line < len(lines) {
						typeError.Errors[i] = "  " + typeError.Errors[i] + fmt.Sprintf(" (line %d: %s)", line+1, strings.TrimSpace(lines[line]))
					}
				}
			}
		}

		extraLines = append(extraLines, typeError.Errors...)
		typeError.Errors = extraLines
	}

	return err
}
