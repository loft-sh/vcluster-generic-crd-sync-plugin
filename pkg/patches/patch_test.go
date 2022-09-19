package patches

import (
	"regexp"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/loft-sh/vcluster-generic-crd-plugin/pkg/config"
	yaml "gopkg.in/yaml.v3"
	"gotest.tools/assert"
)

type patchTestCase struct {
	name  string
	patch *config.Patch

	obj1 string
	obj2 string

	nameResolver NameResolver
	expected     string
}

// TODO update tests

func TestPatch(t *testing.T) {
	True := true

	testCases := []*patchTestCase{
		{
			name: "copy merge",
			patch: &config.Patch{
				Operation: config.PatchTypeCopyFromObject,
				FromPath:  "status.test",
				Path:      "test",
			},
			obj1: `spec: {}
test:
    abc: def`,
			obj2: `status:
    test: test`,
			expected: `spec: {}
test: test`,
		},
		{
			name: "copy",
			patch: &config.Patch{
				Operation: config.PatchTypeCopyFromObject,
				FromPath:  "status",
				Path:      "status",
			},
			obj1: `spec: {}`,
			obj2: `status:
    test: test`,
			expected: `spec: {}
status:
    test: test`,
		},
		{
			name: "simple",
			patch: &config.Patch{
				Operation: config.PatchTypeReplace,
				Path:      "test.test2",
				Value:     "abc",
			},
			obj1: `test:
    test2: def`,
			expected: `test:
    test2: abc`,
		},
		{
			name: "insert",
			patch: &config.Patch{
				Operation: config.PatchTypeAdd,
				Path:      "test.test2[0].test3",
				Value:     "abc",
			},
			obj1: `test:
    test3: {}
test2: {}`,
			expected: `test:
    test3: {}
    test2:
        - test3: abc
test2: {}`,
		},
		{
			name: "insert slice",
			patch: &config.Patch{
				Operation: config.PatchTypeAdd,
				Path:      "test.test2",
				Value:     "abc",
			},
			obj1: `test: 
    test2: 
        - test`,
			expected: `test:
    test2:
        - test
        - abc`,
		},
		{
			name: "insert slice",
			patch: &config.Patch{
				Operation: config.PatchTypeReplace,
				Path:      "test..abc",
				Value:     "def",
			},
			obj1: `test: 
    test2: 
        - abc: test
        - abc: test2`,
			expected: `test:
    test2:
        - abc: def
        - abc: def`,
		},
		{
			name: "condition",
			patch: &config.Patch{
				Operation: config.PatchTypeReplace,
				Path:      "test.abc",
				Value:     "def",
				Conditions: []*config.PatchCondition{
					{
						Path:  "test.status",
						Empty: &True,
					},
				},
			},
			obj1: `test: 
    abc: test`,
			expected: `test:
    abc: def`,
		},
		{
			name: "condition equal",
			patch: &config.Patch{
				Operation: config.PatchTypeReplace,
				Path:      "test.abc",
				Value:     "def",
				Conditions: []*config.PatchCondition{
					{
						Path: "test.status",
						Equal: map[string]interface{}{
							"test": "test",
						},
					},
				},
			},
			obj1: `test: 
    status:
        test: test
    abc: test`,
			expected: `test:
    status:
        test: test
    abc: def`,
		},
		{
			name: "condition equal",
			patch: &config.Patch{
				Operation: config.PatchTypeReplace,
				Path:      "test.abc",
				Value:     "def",
				Conditions: []*config.PatchCondition{
					{
						Path: "test.status",
						Equal: map[string]interface{}{
							"test": "test1",
						},
					},
				},
			},
			obj1: `test: 
    status:
        test: test
    abc: test`,
			expected: `test:
    status:
        test: test
    abc: test`,
		},
		{
			name: "resolve label selector",
			patch: &config.Patch{
				Operation: config.PatchTypeRewriteLabelSelector,
				Path:      "test.abc",
			},
			nameResolver: &fakeNameResolver{},
			obj1: `test:
    abc: {}`,
			expected: `test:
    abc:
        test: test`,
		},
		{
			name: "resolve empty label selector",
			patch: &config.Patch{
				Operation: config.PatchTypeRewriteLabelSelector,
				Path:      "test.abc",
			},
			nameResolver: &fakeNameResolver{},
			obj1: `test:
    abc: null`,
			expected: `test:
    abc: null`,
		},
		{
			name: "resolve filled label selector",
			patch: &config.Patch{
				Operation: config.PatchTypeRewriteLabelSelector,
				Path:      "test.abc",
			},
			nameResolver: &fakeNameResolver{},
			obj1: `test:
    abc:
        test123: test123`,
			expected: `test:
    abc:
        test: test
        test123: test123`,
		},
	}

	for _, testCase := range testCases {
		obj1, err := NewNodeFromString(testCase.obj1)
		assert.NilError(t, err, "error in node creation in test case %s", testCase.name)

		var obj2 *yaml.Node
		if testCase.obj2 != "" {
			obj2, err = NewNodeFromString(testCase.obj2)
			assert.NilError(t, err, "error in node creation in test case %s", testCase.name)
		}

		err = applyPatch(obj1, obj2, testCase.patch, testCase.nameResolver)
		assert.NilError(t, err, "error in applying patch in test case %s", testCase.name)

		// compare output
		out, err := yaml.Marshal(obj1)
		assert.NilError(t, err, "error in yaml marshal in test case %s", testCase.name)
		assert.Equal(t, strings.TrimSpace(string(out)), testCase.expected, "error in comparison in test case %s", testCase.name)
	}
}

type fakeNameResolver struct{}

func (f *fakeNameResolver) TranslateName(name string, _ *regexp.Regexp, path string) (string, error) {
	return name, nil
}

func (f *fakeNameResolver) TranslateLabelExpressionsSelector(selector *metav1.LabelSelector) (*metav1.LabelSelector, error) {
	if selector == nil {
		return nil, nil
	}

	if selector.MatchLabels == nil {
		selector.MatchLabels = map[string]string{}
	}
	selector.MatchLabels["test"] = "test"
	return selector, nil
}

func (f *fakeNameResolver) TranslateLabelSelector(selector map[string]string) (map[string]string, error) {
	if selector == nil {
		return nil, nil
	}
	selector["test"] = "test"
	return selector, nil
}

func (f *fakeNameResolver) TranslateNamespaceRef(name string) (string, error) {
	return "default", nil
}
