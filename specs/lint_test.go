package specs

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"ecctl/pkg/spec"
)

var (
	cachedSpecs     []specFile
	cachedSpecsOnce sync.Once
)

func specsDir() string {
	wd, _ := os.Getwd()
	if filepath.Base(wd) == "specs" {
		return wd
	}
	return filepath.Join(wd, "specs")
}

func docsDir() string {
	wd, _ := os.Getwd()
	if filepath.Base(wd) == "specs" {
		return filepath.Join(wd, "..", "docs", "design")
	}
	return filepath.Join(wd, "docs", "design")
}

type specFile struct {
	product  string
	resource string
	path     string
	raw      []byte
	loaded   spec.ResourceSpec
}

func loadAllSpecs(t *testing.T) []specFile {
	t.Helper()
	cachedSpecsOnce.Do(func() {
		root := specsDir()
		entries, err := os.ReadDir(root)
		if err != nil {
			return
		}
		for _, productEntry := range entries {
			if !productEntry.IsDir() {
				continue
			}
			product := productEntry.Name()
			productDir := filepath.Join(root, product)
			resources, err := os.ReadDir(productDir)
			if err != nil {
				continue
			}
			for _, resEntry := range resources {
				name := resEntry.Name()
				if !strings.HasSuffix(name, ".yaml") || name == "product.yaml" {
					continue
				}
				path := filepath.Join(productDir, name)
				raw, err := os.ReadFile(path)
				if err != nil {
					continue
				}
				loaded, err := spec.Load(raw)
				if err != nil {
					continue
				}
				cachedSpecs = append(cachedSpecs, specFile{
					product:  product,
					resource: strings.TrimSuffix(name, ".yaml"),
					path:     path,
					raw:      raw,
					loaded:   loaded,
				})
			}
		}
	})
	if len(cachedSpecs) == 0 {
		t.Fatal("no spec files found")
	}
	return cachedSpecs
}

func TestSpecLintOperationsOrder(t *testing.T) {
	standardOrder := []string{"create", "update", "delete", "get", "list"}
	orderIndex := map[string]int{}
	for i, name := range standardOrder {
		orderIndex[name] = i
	}

	reOp := regexp.MustCompile(`(?m)^  ([a-z][a-z_]*):\s*$`)

	for _, sf := range loadAllSpecs(t) {
		t.Run(sf.product+"/"+sf.resource, func(t *testing.T) {
			inOps := false
			var ops []string
			for _, line := range strings.Split(string(sf.raw), "\n") {
				if strings.TrimSpace(line) == "operations:" {
					inOps = true
					continue
				}
				if !inOps {
					continue
				}
				if len(line) > 0 && line[0] != ' ' && line[0] != '#' {
					break
				}
				matches := reOp.FindStringSubmatch(line)
				if matches != nil {
					ops = append(ops, matches[1])
				}
			}

			lastIdx := -1
			lastBase := ""
			for _, op := range ops {
				idx, isBase := orderIndex[op]
				if isBase {
					if idx < lastIdx {
						t.Errorf("operation '%s' appears after '%s' (expected order: create, update, delete, get, list)", op, lastBase)
					}
					lastIdx = idx
					lastBase = op
				}
			}

			lastBasePos := -1
			lastBaseName := ""
			for i, op := range ops {
				if _, isBase := orderIndex[op]; isBase {
					lastBasePos = i
					lastBaseName = op
				}
			}
			if lastBasePos >= 0 {
				for i, op := range ops {
					if _, isBase := orderIndex[op]; !isBase && i > 0 && i < lastBasePos {
						t.Errorf("custom action '%s' appears before base action '%s' (custom actions must follow all base actions)", op, lastBaseName)
					}
				}
			}
		})
	}
}

func TestSpecLintFlagNameKebabCase(t *testing.T) {
	reFlag := regexp.MustCompile(`(?m)^\s+flag_name:\s*(\S+)`)

	for _, sf := range loadAllSpecs(t) {
		t.Run(sf.product+"/"+sf.resource, func(t *testing.T) {
			matches := reFlag.FindAllStringSubmatch(string(sf.raw), -1)
			for _, m := range matches {
				flagName := m[1]
				if strings.Contains(flagName, "_") {
					t.Errorf("flag_name '%s' contains underscore (must be kebab-case)", flagName)
				}
				if strings.ToLower(flagName) != flagName {
					t.Errorf("flag_name '%s' contains uppercase (must be kebab-case)", flagName)
				}
			}
		})
	}
}

func TestSpecLintLimitDefault(t *testing.T) {
	for _, sf := range loadAllSpecs(t) {
		ctrl, ok := sf.loaded.Controls["limit"]
		if !ok {
			continue
		}
		t.Run(sf.product+"/"+sf.resource, func(t *testing.T) {
			if ctrl.Max == nil {
				t.Errorf("limit control missing max declaration (rule §4: display API upper bound in help)")
			}

			defaultVal := 0
			switch v := ctrl.Default.(type) {
			case int:
				defaultVal = v
			case int64:
				defaultVal = int(v)
			case uint64:
				defaultVal = int(v)
			case float64:
				defaultVal = int(v)
			default:
				t.Errorf("limit default is not a number: %v (%T)", ctrl.Default, ctrl.Default)
				return
			}

			maxVal := 100
			if ctrl.Max != nil {
				maxVal = *ctrl.Max
			}

			if maxVal < defaultVal {
				t.Errorf("limit max=%d is less than default=%d (max must be >= default)", maxVal, defaultVal)
			}

			if maxVal < 100 {
				if defaultVal != maxVal {
					t.Errorf("limit default=%d but max=%d (should equal max when max < 100)", defaultVal, maxVal)
				}
			} else {
				if defaultVal != 100 {
					t.Errorf("limit default=%d (should be 100)", defaultVal)
				}
			}
		})
	}
}

func TestSpecLintPaginationMutualExclusion(t *testing.T) {
	reControl := regexp.MustCompile(`(?m)^\s+- (page|next_token)\s*$`)

	for _, sf := range loadAllSpecs(t) {
		t.Run(sf.product+"/"+sf.resource, func(t *testing.T) {
			inOps := false
			inList := false
			inControls := false
			hasPage := false
			hasNextToken := false

			for _, line := range strings.Split(string(sf.raw), "\n") {
				trimmed := strings.TrimSpace(line)
				if trimmed == "operations:" {
					inOps = true
					continue
				}
				if !inOps {
					continue
				}
				if regexp.MustCompile(`^  [a-z]`).MatchString(line) {
					if inList && hasPage && hasNextToken {
						t.Errorf("list operation has both --page and --next-token controls")
					}
					inList = strings.HasPrefix(trimmed, "list:")
					inControls = false
					hasPage = false
					hasNextToken = false
				}
				if inList && strings.Contains(line, "controls:") {
					inControls = true
				}
				if inList && inControls {
					if reControl.MatchString(line) {
						m := reControl.FindStringSubmatch(line)
						if m[1] == "page" {
							hasPage = true
						} else if m[1] == "next_token" {
							hasNextToken = true
						}
					}
				}
			}
			if inList && hasPage && hasNextToken {
				t.Errorf("list operation has both --page and --next-token controls")
			}
		})
	}
}

func TestSpecLintForceDefaultFalse(t *testing.T) {
	for _, sf := range loadAllSpecs(t) {
		ctrl, ok := sf.loaded.Controls["force"]
		if !ok {
			continue
		}
		t.Run(sf.product+"/"+sf.resource, func(t *testing.T) {
			switch v := ctrl.Default.(type) {
			case bool:
				if v != false {
					t.Errorf("force default=%v (must be false)", v)
				}
			default:
				t.Errorf("force default is not boolean: %v (%T)", ctrl.Default, ctrl.Default)
			}
		})
	}
}

func TestSpecLintOutputRootSnakeCase(t *testing.T) {
	for _, sf := range loadAllSpecs(t) {
		one := sf.loaded.Identity.OutputRoot.One
		many := sf.loaded.Identity.OutputRoot.Many
		if one == "" && many == "" {
			continue
		}
		t.Run(sf.product+"/"+sf.resource, func(t *testing.T) {
			if strings.Contains(one, "-") || strings.ToLower(one) != one {
				t.Errorf("output_root.one='%s' is not snake_case", one)
			}
			if strings.Contains(many, "-") || strings.ToLower(many) != many {
				t.Errorf("output_root.many='%s' is not snake_case", many)
			}
		})
	}
}

func TestSpecLintLongResourceNamesHaveAliases(t *testing.T) {
	for _, sf := range loadAllSpecs(t) {
		if !strings.Contains(sf.loaded.Resource, "-") {
			continue
		}
		t.Run(sf.product+"/"+sf.resource, func(t *testing.T) {
			if len(sf.loaded.Aliases) == 0 {
				t.Errorf("resource '%s' contains hyphen but has no aliases", sf.loaded.Resource)
			}
		})
	}
}

func TestSpecLintNextTokenAfterLimit(t *testing.T) {
	for _, sf := range loadAllSpecs(t) {
		t.Run(sf.product+"/"+sf.resource, func(t *testing.T) {
			inOps := false
			inList := false
			inControls := false
			var controls []string

			for _, line := range strings.Split(string(sf.raw), "\n") {
				trimmed := strings.TrimSpace(line)
				if trimmed == "operations:" {
					inOps = true
					continue
				}
				if !inOps {
					continue
				}
				if regexp.MustCompile(`^  [a-z]`).MatchString(line) {
					if inList && inControls {
						checkNextTokenOrder(t, controls)
					}
					inList = strings.HasPrefix(trimmed, "list:")
					inControls = false
					controls = nil
				}
				if inList && strings.Contains(line, "controls:") {
					inControls = true
					continue
				}
				if inList && inControls {
					m := regexp.MustCompile(`^\s+- (\w+)`).FindStringSubmatch(line)
					if m != nil {
						controls = append(controls, m[1])
					}
				}
			}
			if inList && inControls {
				checkNextTokenOrder(t, controls)
			}
		})
	}
}

func checkNextTokenOrder(t *testing.T, controls []string) {
	t.Helper()
	limitIdx := -1
	nextTokenIdx := -1
	for i, c := range controls {
		if c == "limit" {
			limitIdx = i
		}
		if c == "next_token" {
			nextTokenIdx = i
		}
	}
	if nextTokenIdx >= 0 && limitIdx >= 0 && nextTokenIdx != limitIdx+1 {
		t.Errorf("next_token (pos %d) must immediately follow limit (pos %d) in list controls: %v", nextTokenIdx, limitIdx, controls)
	}
}

func TestSpecLintDeleteEmitsDeleted(t *testing.T) {
	for _, sf := range loadAllSpecs(t) {
		if _, hasDelete := sf.loaded.Operations["delete"]; !hasDelete {
			continue
		}
		t.Run(sf.product+"/"+sf.resource, func(t *testing.T) {
			inOps := false
			inDelete := false
			hasEmit := false
			hasDeleted := false

			for _, line := range strings.Split(string(sf.raw), "\n") {
				trimmed := strings.TrimSpace(line)
				if trimmed == "operations:" {
					inOps = true
					continue
				}
				if !inOps {
					continue
				}
				if regexp.MustCompile(`^  [a-z]`).MatchString(line) {
					if inDelete && hasEmit && !hasDeleted {
						t.Errorf("delete operation has emit but missing 'deleted: true'")
					}
					if inDelete && !hasEmit {
						t.Errorf("delete operation missing emit with 'deleted: true'")
					}
					inDelete = strings.HasPrefix(trimmed, "delete:")
					hasEmit = false
					hasDeleted = false
				}
				if inDelete {
					if strings.Contains(line, "emit:") {
						hasEmit = true
					}
					if strings.Contains(line, "deleted: true") {
						hasDeleted = true
					}
				}
			}
			if inDelete && hasEmit && !hasDeleted {
				t.Errorf("delete operation has emit but missing 'deleted: true'")
			}
			if inDelete && !hasEmit {
				t.Errorf("delete operation missing emit with 'deleted: true'")
			}
		})
	}
}

func TestSpecLintResponseFieldsSnakeCase(t *testing.T) {
	reFieldKey := regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

	for _, sf := range loadAllSpecs(t) {
		t.Run(sf.product+"/"+sf.resource, func(t *testing.T) {
			for probeName, probe := range sf.loaded.Probes {
				for fieldName := range probe.Response.Fields {
					if fieldName == "from" || fieldName == "each" {
						continue
					}
					if !reFieldKey.MatchString(fieldName) {
						t.Errorf("probe '%s' response field '%s' is not snake_case", probeName, fieldName)
					}
				}
			}
		})
	}
}

func TestSpecLintOperationOutputKeysSnakeCase(t *testing.T) {
	reSnake := regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

	for _, sf := range loadAllSpecs(t) {
		t.Run(sf.product+"/"+sf.resource, func(t *testing.T) {
			for opName, operation := range sf.loaded.Operations {
				for fieldName := range operation.Output.Fields {
					if !reSnake.MatchString(fieldName) {
						t.Errorf("operation '%s' output field '%s' is not snake_case", opName, fieldName)
					}
				}
				for i, selectSpec := range operation.Output.Select {
					if selectSpec.SingleKey != "" && !reSnake.MatchString(selectSpec.SingleKey) {
						t.Errorf("operation '%s' output select %d single_key '%s' is not snake_case", opName, i, selectSpec.SingleKey)
					}
					if selectSpec.ManyKey != "" && !reSnake.MatchString(selectSpec.ManyKey) {
						t.Errorf("operation '%s' output select %d many_key '%s' is not snake_case", opName, i, selectSpec.ManyKey)
					}
					for _, fieldName := range selectSpec.Fields {
						if !reSnake.MatchString(fieldName) {
							t.Errorf("operation '%s' output select %d field '%s' is not snake_case", opName, i, fieldName)
						}
					}
				}
			}
		})
	}
}

func TestSpecLintSchemaFieldsSnakeCase(t *testing.T) {
	reSnake := regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

	for _, sf := range loadAllSpecs(t) {
		t.Run(sf.product+"/"+sf.resource, func(t *testing.T) {
			for fieldName := range sf.loaded.Schema.Fields {
				if !reSnake.MatchString(fieldName) {
					t.Errorf("schema field '%s' is not snake_case", fieldName)
				}
			}
		})
	}
}

func TestSpecLintCloudResourceRefsNoIDSuffix(t *testing.T) {
	cloudResources := map[string]bool{
		"vpc": true, "vswitch": true, "sg": true, "instance": true,
		"disk": true, "image": true, "snapshot": true, "eni": true,
		"keypair": true, "subnet": true, "vpd": true, "vcc": true,
		"er": true, "node": true, "cluster": true,
	}

	for _, sf := range loadAllSpecs(t) {
		t.Run(sf.product+"/"+sf.resource, func(t *testing.T) {
			for fieldName := range sf.loaded.Schema.Fields {
				if !strings.HasSuffix(fieldName, "_id") {
					continue
				}
				base := strings.TrimSuffix(fieldName, "_id")
				if cloudResources[base] {
					t.Errorf("schema field '%s' references cloud resource '%s' — should use '%s' without _id suffix (rule §4)", fieldName, base, base)
				}
			}
		})
	}
}

func TestSpecLintActionVocabulary(t *testing.T) {
	allowedActions := map[string]bool{
		"list": true, "get": true, "create": true, "update": true, "delete": true,
		"enable": true, "disable": true,
		"start": true, "stop": true, "reboot": true,
		"pause": true, "resume": true,
		"install": true,
		"invoke":  true, "exec": true, "sendfile": true,
		"apply": true, "remove": true,
		"authorize": true, "revoke": true,
		"attach": true, "detach": true,
		"copy": true, "clone": true, "export": true, "import": true,
		"renew": true, "monitor": true,
		"reset": true, "reinit": true, "redeploy": true,
		"upgrade": true, "repair": true,
		"cancel": true, "end": true,
	}

	for _, sf := range loadAllSpecs(t) {
		t.Run(sf.product+"/"+sf.resource, func(t *testing.T) {
			for opName := range sf.loaded.Operations {
				if !allowedActions[opName] {
					t.Errorf("operation '%s' is not in the CLI action vocabulary (rule §2)", opName)
				}
			}
		})
	}
}

func TestSpecLintListHasFilterControl(t *testing.T) {
	for _, sf := range loadAllSpecs(t) {
		listOp, hasList := sf.loaded.Operations["list"]
		if !hasList {
			continue
		}
		if len(listOp.Filters) == 0 {
			continue
		}
		t.Run(sf.product+"/"+sf.resource, func(t *testing.T) {
			if _, ok := sf.loaded.Controls["filter"]; !ok {
				t.Errorf("list operation defines filters but no 'filter' control exists")
			}
		})
	}
}

func TestSpecLintListHasLimitControl(t *testing.T) {
	for _, sf := range loadAllSpecs(t) {
		if _, hasList := sf.loaded.Operations["list"]; !hasList {
			continue
		}
		_, hasLimit := sf.loaded.Controls["limit"]
		_, hasPage := sf.loaded.Controls["page"]
		_, hasNextToken := sf.loaded.Controls["next_token"]
		if !hasLimit && !hasPage && !hasNextToken {
			continue
		}
		t.Run(sf.product+"/"+sf.resource, func(t *testing.T) {
			if !hasLimit {
				t.Errorf("resource has pagination controls but no 'limit' control defined")
			}
		})
	}
}

func TestSpecLintListInputControls(t *testing.T) {
	for _, sf := range loadAllSpecs(t) {
		listOp, hasList := sf.loaded.Operations["list"]
		if !hasList {
			continue
		}
		t.Run(sf.product+"/"+sf.resource, func(t *testing.T) {
			controls := operationControlNames(listOp)
			hasFilter := containsString(controls, "filter")
			hasLimit := containsString(controls, "limit")
			hasPage := containsString(controls, "page")
			hasNextToken := containsString(controls, "next_token")

			if len(listOp.Filters) > 0 && !hasFilter {
				t.Errorf("list operation defines filters but input.controls does not include filter")
			}
			if hasPage && hasNextToken {
				t.Errorf("list operation input.controls includes both page and next_token")
			}
			if (hasPage || hasNextToken) && !hasLimit {
				t.Errorf("list operation input.controls includes pagination without limit")
			}
			if hasNextToken {
				limitIdx := indexOfString(controls, "limit")
				nextTokenIdx := indexOfString(controls, "next_token")
				if limitIdx >= 0 && nextTokenIdx != limitIdx+1 {
					t.Errorf("next_token (pos %d) must immediately follow limit (pos %d) in list input.controls: %v", nextTokenIdx, limitIdx, controls)
				}
			}
		})
	}
}

func operationControlNames(operation spec.Operation) []string {
	names := make([]string, 0, len(operation.Input.Controls))
	for _, control := range operation.Input.Controls {
		names = append(names, control.Name)
	}
	return names
}

func containsString(values []string, target string) bool {
	return indexOfString(values, target) >= 0
}

func indexOfString(values []string, target string) int {
	for i, value := range values {
		if value == target {
			return i
		}
	}
	return -1
}

func TestSpecLintGetHasPositionalID(t *testing.T) {
	for _, sf := range loadAllSpecs(t) {
		op, hasGet := sf.loaded.Operations["get"]
		if !hasGet {
			continue
		}
		if len(op.Input.Fields) == 0 {
			continue
		}
		t.Run(sf.product+"/"+sf.resource, func(t *testing.T) {
			hasPositional := false
			for _, field := range op.Input.Fields {
				if field.Positional || field.PositionalMany {
					hasPositional = true
					break
				}
			}
			if !hasPositional {
				if !allowsACKResourceFlagTarget(sf, op) {
					t.Errorf("get operation has no positional parameter (rule §4: target uses positional arg)")
				}
			}
		})
	}
}

func TestSpecLintExamplesFormat(t *testing.T) {
	for _, sf := range loadAllSpecs(t) {
		if len(sf.loaded.Examples) == 0 {
			continue
		}
		t.Run(sf.product+"/"+sf.resource, func(t *testing.T) {
			var validPrefixes []string

			resourceName := sf.loaded.Resource
			if sf.loaded.Parent != "" {
				resourceName = sf.loaded.Parent + " " + strings.ReplaceAll(resourceName, sf.loaded.Parent+"-", "")
			}
			validPrefixes = append(validPrefixes, "ecctl "+sf.product+" "+resourceName+" ")

			for _, alias := range sf.loaded.Aliases {
				validPrefixes = append(validPrefixes, "ecctl "+sf.product+" "+alias+" ")
			}

			if sf.product == sf.loaded.Resource {
				validPrefixes = append(validPrefixes, "ecctl "+sf.product+" ")
			}

			for _, ex := range sf.loaded.Examples {
				matched := false
				for _, prefix := range validPrefixes {
					if strings.HasPrefix(ex, prefix) {
						matched = true
						break
					}
				}
				if !matched {
					t.Errorf("example does not match expected prefixes %v: %s", validPrefixes, ex)
				}
			}
		})
	}
}

func TestSpecLintExampleFlagsKebabCase(t *testing.T) {
	reFlag := regexp.MustCompile(`--([a-zA-Z0-9_-]+)`)

	for _, sf := range loadAllSpecs(t) {
		if len(sf.loaded.Examples) == 0 {
			continue
		}
		t.Run(sf.product+"/"+sf.resource, func(t *testing.T) {
			for _, ex := range sf.loaded.Examples {
				flags := reFlag.FindAllStringSubmatch(ex, -1)
				for _, m := range flags {
					flag := m[1]
					if strings.Contains(flag, "_") {
						t.Errorf("example flag '--%s' contains underscore (must be kebab-case): %s", flag, ex)
					}
				}
			}
		})
	}
}

func TestSpecLintPlusPrefixFieldsRepeatable(t *testing.T) {
	for _, sf := range loadAllSpecs(t) {
		t.Run(sf.product+"/"+sf.resource, func(t *testing.T) {
			for fieldName, field := range sf.loaded.Schema.Fields {
				desc := field.Description["en"] + field.Description["zh-CN"]
				if (strings.Contains(desc, "prefix with +") || strings.Contains(desc, "+前缀") ||
					strings.Contains(desc, "+ to assign") || strings.Contains(desc, "+ to add") ||
					strings.Contains(desc, "+ to associate")) &&
					!field.Repeatable {
					t.Errorf("field '%s' uses +- prefix syntax but is not repeatable", fieldName)
				}
			}
		})
	}
}

func TestSpecLintProductResourcesMatchFiles(t *testing.T) {
	root := specsDir()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}

	for _, productEntry := range entries {
		if !productEntry.IsDir() {
			continue
		}
		product := productEntry.Name()
		productYAML := filepath.Join(root, product, "product.yaml")
		raw, err := os.ReadFile(productYAML)
		if err != nil {
			continue
		}

		t.Run(product, func(t *testing.T) {
			productSpec, err := spec.LoadProductSpec(raw)
			if err != nil {
				t.Fatalf("LoadProductSpec: %v", err)
				return
			}

			specFiles := map[string]bool{}
			dirEntries, _ := os.ReadDir(filepath.Join(root, product))
			for _, e := range dirEntries {
				name := e.Name()
				if strings.HasSuffix(name, ".yaml") && name != "product.yaml" {
					specFiles[strings.TrimSuffix(name, ".yaml")] = true
				}
			}

			for _, res := range productSpec.Resources {
				if !specFiles[res] {
					t.Errorf("product.yaml lists resource '%s' but no %s.yaml file exists", res, res)
				}
			}
			for file := range specFiles {
				found := false
				for _, res := range productSpec.Resources {
					if res == file {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("spec file %s.yaml exists but not listed in product.yaml resources", file)
				}
			}
		})
	}
}

func TestSpecLintIdentityOutputRootRequired(t *testing.T) {
	for _, sf := range loadAllSpecs(t) {
		if _, hasList := sf.loaded.Operations["list"]; !hasList {
			continue
		}
		t.Run(sf.product+"/"+sf.resource, func(t *testing.T) {
			if sf.loaded.Identity.OutputRoot.Many == "" {
				t.Errorf("resource has list operation but identity.output_root.many is not defined")
			}
		})
	}
}

func TestSpecLintDesignDocExists(t *testing.T) {
	docs := docsDir()

	for _, sf := range loadAllSpecs(t) {
		t.Run(sf.product+"/"+sf.resource, func(t *testing.T) {
			docPath := designDocPath(docs, sf)
			if _, err := os.Stat(docPath); err != nil {
				if os.IsNotExist(err) {
					t.Errorf("design doc missing: %s", docPath)
					return
				}
				t.Fatalf("Stat(%s): %v", docPath, err)
			}
		})
	}
}

func designDocPath(docs string, sf specFile) string {
	docFile := sf.resource + ".md"
	if sf.loaded.Parent != "" {
		docFile = sf.loaded.Parent + ".md"
	}
	return filepath.Join(docs, sf.product, docFile)
}

func TestSpecLintDesignDocOperationsOrder(t *testing.T) {
	docs := docsDir()
	standardOrder := []string{"create", "update", "delete", "get", "list"}
	orderIndex := map[string]int{}
	for i, name := range standardOrder {
		orderIndex[name] = i
	}

	for _, sf := range loadAllSpecs(t) {
		docFile := sf.resource + ".md"
		if sf.loaded.Parent != "" {
			docFile = sf.loaded.Parent + ".md"
		}
		docPath := filepath.Join(docs, sf.product, docFile)
		raw, err := os.ReadFile(docPath)
		if err != nil {
			continue
		}

		t.Run(sf.product+"/"+sf.resource, func(t *testing.T) {
			resourceCmd := sf.loaded.Resource
			if sf.loaded.Parent != "" {
				resourceCmd = sf.loaded.Parent + " " + strings.ReplaceAll(sf.loaded.Resource, sf.loaded.Parent+"-", "")
			}
			var pattern string
			if sf.product == sf.loaded.Resource {
				pattern = "## `ecctl " + regexp.QuoteMeta(sf.product) + ` ([a-z-]+)` + "(`| <| --)"
			} else {
				pattern = "## `ecctl " + regexp.QuoteMeta(sf.product) + " " + regexp.QuoteMeta(resourceCmd) + ` ([a-z-]+)` + "(`| <| --)"
			}
			reAction := regexp.MustCompile(pattern)

			matches := reAction.FindAllStringSubmatch(string(raw), -1)
			var actions []string
			for _, m := range matches {
				actions = append(actions, m[1])
			}

			lastIdx := -1
			lastBase := ""
			for _, action := range actions {
				idx, isBase := orderIndex[action]
				if isBase {
					if idx < lastIdx {
						t.Errorf("design doc: action '%s' appears after '%s' (expected: create, update, delete, get, list)", action, lastBase)
					}
					lastIdx = idx
					lastBase = action
				}
			}
		})
	}
}

func TestSpecLintResourceNameKebabCase(t *testing.T) {
	reKebab := regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

	for _, sf := range loadAllSpecs(t) {
		t.Run(sf.product+"/"+sf.resource, func(t *testing.T) {
			if !reKebab.MatchString(sf.loaded.Resource) {
				t.Errorf("resource name '%s' is not kebab-case", sf.loaded.Resource)
			}
		})
	}
}

func TestSpecLintCLITokensKebabCase(t *testing.T) {
	reKebab := regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

	for _, sf := range loadAllSpecs(t) {
		t.Run(sf.product+"/"+sf.resource, func(t *testing.T) {
			if !reKebab.MatchString(sf.loaded.Product) {
				t.Errorf("product '%s' is not kebab-case", sf.loaded.Product)
			}
			if sf.loaded.Parent != "" && !reKebab.MatchString(sf.loaded.Parent) {
				t.Errorf("parent '%s' is not kebab-case", sf.loaded.Parent)
			}
			for _, alias := range sf.loaded.Aliases {
				if !reKebab.MatchString(alias) {
					t.Errorf("alias '%s' is not kebab-case", alias)
				}
			}
		})
	}
}

func TestSpecLintExamplesCount(t *testing.T) {
	for _, sf := range loadAllSpecs(t) {
		t.Run(sf.product+"/"+sf.resource, func(t *testing.T) {
			count := len(sf.loaded.Examples)
			if count < 2 || count > 4 {
				t.Errorf("examples count=%d (must be 2-4)", count)
			}
		})
	}
}

func TestSpecLintGetHasOutputRootOne(t *testing.T) {
	for _, sf := range loadAllSpecs(t) {
		if _, hasGet := sf.loaded.Operations["get"]; !hasGet {
			continue
		}
		t.Run(sf.product+"/"+sf.resource, func(t *testing.T) {
			if sf.loaded.Identity.OutputRoot.One == "" {
				t.Errorf("resource has get operation but identity.output_root.one is not defined")
			}
		})
	}
}

func TestSpecLintDescriptionRequired(t *testing.T) {
	for _, sf := range loadAllSpecs(t) {
		t.Run(sf.product+"/"+sf.resource, func(t *testing.T) {
			if sf.loaded.Description.Text("en") == "" {
				t.Errorf("resource description is empty")
			}
		})
	}
}

func TestSpecLintLocalizedTextComplete(t *testing.T) {
	for _, sf := range loadAllSpecs(t) {
		t.Run(sf.product+"/"+sf.resource, func(t *testing.T) {
			requireLocalizedText(t, "display_name", sf.loaded.DisplayName)
			requireLocalizedText(t, "description", sf.loaded.Description)
			for id, message := range sf.loaded.Messages {
				requireLocalizedText(t, "message "+id, message)
			}
			for fieldName, field := range sf.loaded.Schema.Fields {
				requireLocalizedText(t, "schema field "+fieldName+" description", field.Description)
			}
			for controlName, control := range sf.loaded.Controls {
				requireLocalizedText(t, "control "+controlName+" description", control.Description)
			}
			for opName, operation := range sf.loaded.Operations {
				requireLocalizedText(t, "operation "+opName+" description", operation.Description)
			}
		})
	}
}

func requireLocalizedText(t *testing.T, label string, text spec.LocalizedText) {
	t.Helper()
	if strings.TrimSpace(text["en"]) == "" {
		t.Errorf("%s missing en text", label)
	}
	if strings.TrimSpace(text["zh-CN"]) == "" {
		t.Errorf("%s missing zh-CN text", label)
	}
}

func TestSpecLintControlNamesSnakeCase(t *testing.T) {
	reSnake := regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

	for _, sf := range loadAllSpecs(t) {
		t.Run(sf.product+"/"+sf.resource, func(t *testing.T) {
			for name := range sf.loaded.Controls {
				if !reSnake.MatchString(name) {
					t.Errorf("control name '%s' is not snake_case", name)
				}
			}
		})
	}
}

func TestSpecLintExtraFieldsSnakeCase(t *testing.T) {
	reSnake := regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

	for _, sf := range loadAllSpecs(t) {
		t.Run(sf.product+"/"+sf.resource, func(t *testing.T) {
			for probeName, probe := range sf.loaded.Probes {
				for fieldName := range probe.Response.ExtraFields {
					if !reSnake.MatchString(fieldName) {
						t.Errorf("probe '%s' extra_field '%s' is not snake_case", probeName, fieldName)
					}
				}
			}
		})
	}
}

func TestSpecLintDeleteHasPositionalID(t *testing.T) {
	for _, sf := range loadAllSpecs(t) {
		op, hasDelete := sf.loaded.Operations["delete"]
		if !hasDelete {
			continue
		}
		if len(op.Input.Fields) == 0 {
			continue
		}
		t.Run(sf.product+"/"+sf.resource, func(t *testing.T) {
			hasPositional := false
			for _, field := range op.Input.Fields {
				if field.Positional || field.PositionalMany {
					hasPositional = true
					break
				}
			}
			if !hasPositional {
				if !allowsACKResourceFlagTarget(sf, op) {
					t.Errorf("delete operation has no positional parameter (rule §4: target uses positional arg)")
				}
			}
		})
	}
}

func allowsACKResourceFlagTarget(sf specFile, op spec.Operation) bool {
	if sf.product != "ack" {
		return false
	}
	for _, field := range op.Input.Fields {
		if !field.Required {
			continue
		}
		switch field.Name {
		case "cluster", "user_id":
			return true
		case "id":
			if field.FlagName == "cluster" {
				return true
			}
		}
	}
	return false
}

func TestSpecLintProductMatchesDir(t *testing.T) {
	for _, sf := range loadAllSpecs(t) {
		t.Run(sf.product+"/"+sf.resource, func(t *testing.T) {
			if sf.loaded.Product != sf.product {
				t.Errorf("spec product='%s' does not match directory name '%s'", sf.loaded.Product, sf.product)
			}
		})
	}
}

func TestSpecLintResourceMatchesFileName(t *testing.T) {
	for _, sf := range loadAllSpecs(t) {
		t.Run(sf.product+"/"+sf.resource, func(t *testing.T) {
			expectedFile := sf.loaded.Resource
			if sf.loaded.Parent != "" {
				expectedFile = sf.loaded.Parent + "-" + sf.loaded.Resource
			}
			if sf.resource != expectedFile {
				t.Errorf("spec resource='%s' (parent='%s') does not match file name '%s.yaml' (expected '%s.yaml')",
					sf.loaded.Resource, sf.loaded.Parent, sf.resource, expectedFile)
			}
		})
	}
}

func TestSpecLintDesignDocOperationsMatch(t *testing.T) {
	docs := docsDir()

	for _, sf := range loadAllSpecs(t) {
		docFile := sf.resource + ".md"
		if sf.loaded.Parent != "" {
			docFile = sf.loaded.Parent + ".md"
		}
		docPath := filepath.Join(docs, sf.product, docFile)
		raw, err := os.ReadFile(docPath)
		if err != nil {
			continue
		}

		t.Run(sf.product+"/"+sf.resource, func(t *testing.T) {
			resourceCmd := sf.loaded.Resource
			if sf.loaded.Parent != "" {
				resourceCmd = sf.loaded.Parent + " " + strings.ReplaceAll(sf.loaded.Resource, sf.loaded.Parent+"-", "")
			}
			var pattern string
			if sf.product == sf.loaded.Resource {
				pattern = "## `ecctl " + regexp.QuoteMeta(sf.product) + ` ([a-z-]+)` + "(`| <| --)"
			} else {
				pattern = "## `ecctl " + regexp.QuoteMeta(sf.product) + " " + regexp.QuoteMeta(resourceCmd) + ` ([a-z-]+)` + "(`| <| --)"
			}
			reAction := regexp.MustCompile(pattern)

			matches := reAction.FindAllStringSubmatch(string(raw), -1)
			docActions := map[string]bool{}
			for _, m := range matches {
				docActions[m[1]] = true
			}

			specOps := map[string]bool{}
			inOps := false
			for _, line := range strings.Split(string(sf.raw), "\n") {
				if strings.TrimSpace(line) == "operations:" {
					inOps = true
					continue
				}
				if inOps {
					m := regexp.MustCompile(`^  ([a-z][a-z_]*):`).FindStringSubmatch(line)
					if m != nil {
						specOps[m[1]] = true
					} else if len(line) > 0 && line[0] != ' ' && line[0] != '#' {
						break
					}
				}
			}

			for action := range docActions {
				if !specOps[action] {
					t.Errorf("action '%s' in design doc but not in spec", action)
				}
			}
			for op := range specOps {
				if !docActions[op] {
					t.Errorf("operation '%s' in spec but not in design doc", op)
				}
			}
		})
	}
}
