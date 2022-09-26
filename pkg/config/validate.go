package config

import (
	"fmt"

	"github.com/loft-sh/vcluster-generic-crd-plugin/pkg/util/yaml"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func ParseConfig(rawConfig string) (*Config, error) {
	configuration := &Config{}
	err := yaml.UnmarshalStrict([]byte(rawConfig), configuration)
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
		if mapping.FromVirtualCluster.APIVersion == "" {
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
	if syncBack.APIVersion == "" {
		return fmt.Errorf("apiVersion is required")
	}

	gvk := schema.FromAPIVersionAndKind(syncBack.APIVersion, syncBack.Kind)
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
	case PatchTypeRewriteName, PatchTypeRewriteLabelSelector, PatchTypeRewriteLabelExpressionsSelector:
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
