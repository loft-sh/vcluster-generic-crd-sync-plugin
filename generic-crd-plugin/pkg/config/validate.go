package config

import (
	"bytes"
	"fmt"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"regexp"
	"strconv"
	"strings"
)

func ParseConfig(rawConfig string) (*Config, error) {
	configuration := &Config{}
	err := UnmarshalStrict([]byte(rawConfig), configuration)
	if err != nil {
		return nil, errors.Wrap(err, "parse config")
	}

	err = validate(configuration)
	if err != nil {
		return nil, err
	}

	return configuration, nil
}

func validate(config *Config) error {
	if config.Version != Version {
		return fmt.Errorf("unsupported configuration version. Only %s is supported by this plugin version", config.Version)
	}

	for idx, mapping := range config.Mappings {
		if mapping.FromVirtualCluster == nil {
			return fmt.Errorf("mappings[%d].fromVirtualCluster is required", idx)
		}
		if mapping.FromVirtualCluster.Kind == "" {
			return fmt.Errorf("mappings[%d].fromVirtualCluster.kind is required", idx)
		}
		if mapping.FromVirtualCluster.ApiVersion == "" {
			return fmt.Errorf("mappings[%d].fromVirtualCluster.apiVersion is required", idx)
		}

		for patchIdx, patch := range mapping.FromVirtualCluster.Patches {
			err := validatePatch(patch)
			if err != nil {
				return errors.Wrapf(err, "mappings[%d].fromVirtualCluster.patches[%d]", idx, patchIdx)
			}
		}
		for patchIdx, patch := range mapping.FromVirtualCluster.ReversePatches {
			err := validatePatch(patch)
			if err != nil {
				return errors.Wrapf(err, "mappings[%d].fromVirtualCluster.reversePatches[%d]", idx, patchIdx)
			}
		}

		// make sure we don't have multiple sync backs with the same apiVersion / kind
		uniqueSyncBacks := map[schema.GroupVersionKind]bool{}
		for syncBackIdx, syncBack := range mapping.FromVirtualCluster.SyncBack {
			err := validateSyncBack(syncBack, uniqueSyncBacks)
			if err != nil {
				return errors.Wrapf(err, "mappings[%d].fromVirtualCluster.syncBack[%d]", idx, syncBackIdx)
			}
		}
	}

	return nil
}

func validateSyncBack(syncBack *SyncBack, uniqueSyncBacks map[schema.GroupVersionKind]bool) error {
	if syncBack.Kind == "" {
		return fmt.Errorf("kind is required")
	}
	if syncBack.ApiVersion == "" {
		return fmt.Errorf("apiVersion is required")
	}

	gvk := schema.FromAPIVersionAndKind(syncBack.ApiVersion, syncBack.Kind)
	if uniqueSyncBacks[gvk] {
		return fmt.Errorf("another syncBack with the same kind and apiVersion already exists")
	}
	uniqueSyncBacks[gvk] = true

	for patchIdx, patch := range syncBack.Patches {
		err := validatePatch(patch)
		if err != nil {
			return errors.Wrapf(err, "patches[%d]", patchIdx)
		}
	}
	for patchIdx, patch := range syncBack.ReversePatches {
		err := validatePatch(patch)
		if err != nil {
			return errors.Wrapf(err, "reversePatches[%d]", patchIdx)
		}
	}
	return nil
}

func validatePatch(patch *Patch) error {
	switch patch.Operation {
	case PatchTypeRemove, PatchTypeReplace, PatchTypeAdd:
		if patch.FromPath != "" {
			return fmt.Errorf("fromPath is not supported for this operation")
		}

		return nil
	case PatchTypeRewriteName:
		return nil
	case PatchTypeCopyFromObject:
		if patch.FromPath == "" {
			return fmt.Errorf("fromPath is required for this operation")
		}

		return nil
	default:
		return fmt.Errorf("unsupported patch type %s", patch.Operation)
	}
}

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
