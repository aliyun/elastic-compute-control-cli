package spec

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"ecctl/pkg/i18n"
)

func TestLoadResourceSpecFromYAML(t *testing.T) {
	raw := []byte(`
schema_version: 2
product: ecs
resource: instance
kind: regional
aliases: [vm]
identity:
  field: id
  prefix: i-
  output_root:
    one: instance
    many: instances
schema:
  fields:
    type:
      type: string
operations:
  create:
    input:
      fields:
        - type:
            required: true
bindings:
  create:
    api: RunInstances
    request:
      InstanceType: $.type
`)

	loaded, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Product != "ecs" || loaded.Resource != "instance" {
		t.Fatalf("loaded wrong resource: %#v", loaded)
	}
	if loaded.Identity.OutputRoot.One != "instance" || loaded.Identity.OutputRoot.Many != "instances" {
		t.Fatalf("output roots not loaded: %#v", loaded.Identity.OutputRoot)
	}
	if got := loaded.Operations["create"].Input.Fields[0]; got.Name != "type" || !got.Required {
		t.Fatalf("operation input not loaded: %#v", loaded.Operations["create"].Input.Fields)
	}
}

func TestLoadDefaultProductUsesGeneratedCatalog(t *testing.T) {
	ResetCacheForTest()
	product, err := LoadProduct("", "ecs")
	if err != nil {
		t.Fatalf("LoadProduct: %v", err)
	}
	if product.Product != "ecs" || product.SchemaVersion == 0 {
		t.Fatalf("product = %#v", product)
	}
}

func TestLoadResourceSchemaControlsAndOperations(t *testing.T) {
	raw := []byte(`
schema_version: 2
product: ecs
resource: instance
kind: regional
schema:
  fields:
    image:
      type: string
      description:
        en: ECS image ID
        zh-CN: ECS 镜像 ID
    data_disks:
      type: array
      description:
        en: Data disks.
        zh-CN: 数据盘列表。
      items:
        type: object
        fields:
          category:
            type: string
            description:
              en: Data disk category.
              zh-CN: 数据盘类型。
controls:
  api_param:
    type: key_value
    repeatable: true
  timeout:
    type: duration
    default: 300s
operations:
  create:
    examples:
      - ecctl ecs instance create --image i-1
    input:
      fields:
        - image:
            required: true
        - data_disks
      controls:
        - api_param
        - timeout
    workflow:
      - binding: create
bindings:
  create:
    api: RunInstances
    request:
      ImageId: $.image
      DataDisk:
        each: $.data_disks
        fields:
          Category: $.category
`)

	loaded, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := Validate(loaded); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	dataDisks := loaded.Schema.Fields["data_disks"]
	if dataDisks.Items == nil || dataDisks.Items.Fields["category"].Type != "string" {
		t.Fatalf("data_disks schema = %#v", dataDisks)
	}
	fields := loaded.Operations["create"].Input.Fields
	if len(fields) != 2 || fields[0].Name != "image" || fields[1].Name != "data_disks" {
		t.Fatalf("operation fields = %#v", fields)
	}
	if !fields[0].Required || !fields[0].HasRequired {
		t.Fatalf("image required override = %#v", fields[0])
	}
	controls := loaded.Operations["create"].Input.Controls
	if len(controls) != 2 || controls[0].Name != "api_param" || controls[1].Name != "timeout" {
		t.Fatalf("operation controls = %#v", controls)
	}
}

func TestLoadSupportsHookAPICalls(t *testing.T) {
	raw := []byte(`
schema_version: 2
product: demo
resource: thing
kind: regional
schema:
  fields:
    name:
      type: string
bindings:
  create:
    api: CreateThing
    hooks:
      before: [resolve_name]
      api_calls:
        - hook: resolve_name
          api: DescribeThings
          phase: preflight
          condition: input.name != ""
          purpose:
            en: resolve the thing name
            zh-CN: 解析对象名称
    request: {}
operations:
  create:
    examples: [ecctl demo thing create --name example]
    input:
      fields: [name]
    workflow:
      - binding: create
`)

	loaded, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := Validate(loaded); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	calls := loaded.Bindings["create"].Hooks.APICalls
	if len(calls) != 1 {
		t.Fatalf("hook api calls = %#v, want one entry", calls)
	}
	call := calls[0]
	if call.Hook != "resolve_name" || call.API != "DescribeThings" || call.Phase != "preflight" || call.Condition != `input.name != ""` {
		t.Fatalf("hook api call = %#v", call)
	}
	if call.Purpose.Text("en") != "resolve the thing name" || call.Purpose.Text("zh-CN") != "解析对象名称" {
		t.Fatalf("hook api call purpose = %#v", call.Purpose)
	}
}

func TestValidateRejectsInvalidHookAPICalls(t *testing.T) {
	tests := []struct {
		name       string
		before     []string
		afterError []string
		call       HookAPICall
		want       string
	}{
		{name: "missing hook", before: []string{"resolve_name"}, call: HookAPICall{API: "DescribeThings", Phase: "preflight", Purpose: LocalizedText{"en": "resolve name"}}, want: "hook is required"},
		{name: "unattached hook", before: []string{"resolve_name"}, call: HookAPICall{Hook: "missing", API: "DescribeThings", Phase: "preflight", Purpose: LocalizedText{"en": "resolve name"}}, want: `references unattached before hook "missing"`},
		{name: "after error is not preflight", afterError: []string{"resolve_name"}, call: HookAPICall{Hook: "resolve_name", API: "DescribeThings", Phase: "preflight", Purpose: LocalizedText{"en": "resolve name"}}, want: `references unattached before hook "resolve_name"`},
		{name: "missing api", before: []string{"resolve_name"}, call: HookAPICall{Hook: "resolve_name", Phase: "preflight", Purpose: LocalizedText{"en": "resolve name"}}, want: "api is required"},
		{name: "unsupported phase", before: []string{"resolve_name"}, call: HookAPICall{Hook: "resolve_name", API: "DescribeThings", Phase: "wait", Purpose: LocalizedText{"en": "resolve name"}}, want: `phase "wait" is not supported`},
		{name: "missing purpose", before: []string{"resolve_name"}, call: HookAPICall{Hook: "resolve_name", API: "DescribeThings", Phase: "preflight"}, want: "purpose is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := minimalValidSpec()
			binding := spec.Bindings["create"]
			binding.Hooks = BindingHooks{Before: tt.before, AfterError: tt.afterError, APICalls: []HookAPICall{tt.call}}
			spec.Bindings["create"] = binding
			if err := Validate(spec); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestLoadSupportsAPIProductAndProbeExtraFields(t *testing.T) {
	raw := []byte(`
schema_version: 2
product: rg
api_product: ResourceManager
resource: group
kind: regional
schema:
  fields:
    id:
      type: string
probes:
  list:
    api: ListResourceGroupsWithAuthDetails
    response:
      items: $.ResourceGroups
      request_id: $.RequestId
      fields:
        id: $.Id
      extra_fields:
        auth_details: $.AuthDetails
operations:
  list:
    workflow:
      - probe: list
        many: true
`)

	loaded, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.APIProduct != "ResourceManager" {
		t.Fatalf("api_product = %q, want ResourceManager", loaded.APIProduct)
	}
	if got := loaded.Probes["list"].Response.ExtraFields["auth_details"].Path; got != "$.AuthDetails" {
		t.Fatalf("extra field auth_details = %q, want $.AuthDetails", got)
	}
	if err := Validate(loaded); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestLoadRejectsOldSpecSections(t *testing.T) {
	raw := []byte(`
schema_version: 2
product: ecs
resource: instance
kind: regional
params:
  image:
    type: string
actions: {}
`)

	if _, err := Load(raw); err == nil {
		t.Fatal("Load succeeded with old params/actions sections")
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	raw := []byte(`
schema_version: 2
product: ecs
resource: instance
kind: regional
schema:
  fields:
    name:
      type: string
unexpected_field: true
`)

	if _, err := Load(raw); err == nil {
		t.Fatal("Load succeeded with unknown field")
	}
}

func TestLoadSupportsBindingIdempotencyAndSingleProbeItem(t *testing.T) {
	raw := []byte(`
schema_version: 2
product: vpc
resource: vpc
kind: regional
schema:
  fields:
    id:
      type: string
probes:
  attribute:
    api: DescribeVpcAttribute
    request:
      RegionId: $context.region
      VpcId: $input.id
    response:
      item: $
      request_id: $.RequestId
      id: $.VpcId
      state: $.Status
      fields:
        id: $.VpcId
        name: $.VpcName
bindings:
  create_to_available:
    api: CreateVpc
    idempotency:
      field: ClientToken
      prefix: vpc-create
    request:
      RegionId: $context.region
    id_from: $.VpcId
operations:
  get:
    call:
      probe: attribute
      ids:
        - $input.id
`)

	loaded, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Probes["attribute"].Response.Item != "$" {
		t.Fatalf("probe item = %q, want $", loaded.Probes["attribute"].Response.Item)
	}
	idempotency := loaded.Bindings["create_to_available"].Idempotency
	if idempotency.Field != "ClientToken" || idempotency.Prefix != "vpc-create" {
		t.Fatalf("idempotency = %#v", idempotency)
	}
}

func TestLoadSupportsWaiterPendingFields(t *testing.T) {
	raw := []byte(`
schema_version: 2
product: vpc
resource: vpc
kind: regional
schema:
  fields:
    id:
      type: string
probes:
  attribute:
    api: DescribeVpcAttribute
    response:
      item: $
      state: $.Status
      fields:
        status: $.Status
        dns_hostname_status: $.DnsHostnameStatus
waiters:
  available_after_update:
    probe: attribute
    target: Available
    interval: 2s
    timeout: 300s
    pending:
      - field: dns_hostname_status
        values: [ENABLING, DISABLING]
`)

	loaded, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	pending := loaded.Waiters["available_after_update"].Pending
	if len(pending) != 1 || pending[0].Field != "dns_hostname_status" || !hasString(pending[0].Values, "ENABLING") || !hasString(pending[0].Values, "DISABLING") {
		t.Fatalf("pending = %#v", pending)
	}
}

func TestLoadSupportsEnhancedEachDeclarations(t *testing.T) {
	raw := []byte(`
schema_version: 2
product: ecs
resource: sg
kind: regional
schema:
  fields:
    id:
      type: string
    rule:
      type: array
      items:
        type: string
    protocol:
      type: string
    port:
      type: string
    cidr:
      type: cidr
bindings:
  authorize_rules:
    api: AuthorizeSecurityGroup
    request:
      RegionId: $context.region
      Permissions:
        capture: rule_permissions
        each:
          normalize: security_group_rule
          sources:
            - source: $.rule
            - from_fields:
                protocol: $.protocol
                port: $.port
                cidr: $.cidr
              when_any: [protocol, port, cidr]
          defaults:
            direction: ingress
            policy: accept
            priority: 1
        fields:
          IpProtocol: $.protocol
          PortRange: $.port_range
          SourceCidrIp: $.cidr
operations:
  authorize:
    examples:
      - ecctl ecs sg authorize <sg-id> --rule tcp:22@0.0.0.0/0
    input:
      fields: [id, rule, protocol, port, cidr]
    workflow:
      - binding: authorize_rules
`)

	loaded, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := Validate(loaded); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	permissions, ok := loaded.Bindings["authorize_rules"].Request["Permissions"].(map[string]any)
	if !ok || permissions["capture"] != "rule_permissions" {
		t.Fatalf("permissions request = %#v", loaded.Bindings["authorize_rules"].Request["Permissions"])
	}
}

func TestLocalizedTextMatchesLanguageBaseWithoutLanguageBranches(t *testing.T) {
	text := LocalizedText{"en": "English", "zh-CN": "中文"}

	if got := text.Text("zh-Hans"); got != "中文" {
		t.Fatalf("Text(zh-Hans) = %q, want Chinese base-language match", got)
	}
	if got := text.Text("fr-FR"); got != "English" {
		t.Fatalf("Text(fr-FR) = %q, want English fallback", got)
	}
}

func TestLocalizedTextUnmarshalSupportsStringAndEmptyValues(t *testing.T) {
	raw := []byte(`
schema_version: 1
product: ecs
resource: instance
kind: regional
display_name: Instance
description: ""
`)

	loaded, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := loaded.DisplayName.Text("en"); got != "Instance" {
		t.Fatalf("display_name = %q", got)
	}
	if loaded.Description != nil {
		t.Fatalf("description = %#v, want nil", loaded.Description)
	}
}

func TestProductSpecLoadsDescriptionAndExamples(t *testing.T) {
	raw := []byte(`
schema_version: 1
product: ecs
description:
  en: Manage ECS resources
  zh-CN: 管理 ECS 资源
examples:
  - ecctl ecs instance list
  - ecctl ecs sg list
`)

	loaded, err := LoadProductSpec(raw)
	if err != nil {
		t.Fatalf("LoadProductSpec: %v", err)
	}
	if err := ValidateProduct(loaded); err != nil {
		t.Fatalf("ValidateProduct: %v", err)
	}
	requireLocalizedText(t, "description", loaded.Description)
	if len(loaded.Examples) != 2 {
		t.Fatalf("examples len = %d, want 2", len(loaded.Examples))
	}
}

func TestLoadProductFromSpecDirectory(t *testing.T) {
	dir := t.TempDir()
	productDir := filepath.Join(dir, "ecs")
	if err := os.MkdirAll(productDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	raw := []byte(`
schema_version: 1
product: ecs
description: Elastic Compute Service
examples:
  - ecctl ecs instance list
  - ecctl ecs sg list
`)
	if err := os.WriteFile(filepath.Join(productDir, "product.yaml"), raw, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	loaded, err := LoadProduct(dir, "ecs")
	if err != nil {
		t.Fatalf("LoadProduct: %v", err)
	}
	if loaded.Product != "ecs" || loaded.Description.Text("en") != "Elastic Compute Service" {
		t.Fatalf("loaded = %#v", loaded)
	}
}

func TestLoadProductRejectsProductMismatch(t *testing.T) {
	dir := t.TempDir()
	productDir := filepath.Join(dir, "ecs")
	if err := os.MkdirAll(productDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	raw := []byte(`
schema_version: 1
product: vpc
description: VPC
examples:
  - ecctl vpc list
  - ecctl vpc create
`)
	if err := os.WriteFile(filepath.Join(productDir, "product.yaml"), raw, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := LoadProduct(dir, "ecs"); err == nil {
		t.Fatal("LoadProduct succeeded, want product mismatch error")
	}
}

func TestOperationFieldOptionsLoadEnumsDescriptionsAndDefaults(t *testing.T) {
	raw := []byte(`
schema_version: 2
product: ecs
resource: instance
kind: regional
schema:
  fields:
    type:
      type: string
operations:
  create:
    input:
      fields:
        - type:
            repeatable: true
            positional: true
            positional_many: true
            schema: false
            input_style: select
            default: ecs.e3.medium
            enum: [ecs.e3.medium, ecs.u1]
            description:
              en: Instance type
              zh-CN: 实例规格
`)

	loaded, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	field := loaded.Operations["create"].Input.Fields[0]
	if !field.Repeatable || !field.HasRepeatable || !field.Positional || !field.PositionalMany {
		t.Fatalf("field flags = %#v", field)
	}
	if !field.HasSchema || field.Schema {
		t.Fatalf("field schema option = %#v", field)
	}
	if field.InputStyle != "select" || field.Default != "ecs.e3.medium" || !hasString(field.Enum, "ecs.u1") {
		t.Fatalf("field options = %#v", field)
	}
	if field.Description.Text("zh-Hans") != "实例规格" {
		t.Fatalf("field description = %#v", field.Description)
	}
}

func TestOperationFieldOptionsLoadBriefMetadata(t *testing.T) {
	raw := []byte(`
schema_version: 2
product: demo
resource: widget
kind: regional
schema:
  fields:
    name:
      type: string
      description:
        en: widget name
    advanced:
      type: string
      description:
        en: advanced option
operations:
  create:
    input:
      fields:
        - name:
            required: true
        - advanced:
            brief: false
    workflow: []
`)

	loaded, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	fields := loaded.Operations["create"].Input.Fields
	if fields[1].Name != "advanced" || fields[1].Brief || !fields[1].HasBrief {
		t.Fatalf("brief metadata = %#v", fields[1])
	}
	if !fields[0].Brief || fields[0].HasBrief {
		t.Fatalf("brief default = %#v", fields[0])
	}
}

func TestOperationFieldFlagNameLoads(t *testing.T) {
	raw := []byte(`
schema_version: 2
product: ecs
resource: disk
kind: regional
schema:
  fields:
    encryption_default_action:
      type: string
operations:
  update:
    input:
      fields:
        - encryption_default_action:
            flag_name: encryption_default
    workflow: []
`)

	loaded, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	fields := loaded.Operations["update"].Input.Fields
	if len(fields) != 1 || fields[0].FlagName != "encryption_default" {
		t.Fatalf("operation field flag name = %#v", fields)
	}
}

func TestOperationFieldFlagNameDuplicateUsesCLIName(t *testing.T) {
	raw := []byte(`
schema_version: 2
product: ecs
resource: disk
kind: regional
schema:
  fields:
    encryption_default:
      type: string
    encryption_default_action:
      type: string
operations:
  update:
    input:
      fields:
        - encryption_default
        - encryption_default_action:
            flag_name: encryption-default
    workflow: []
`)

	loaded, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	err = Validate(loaded)
	if err == nil || !strings.Contains(err.Error(), `duplicate flag name "encryption-default"`) {
		t.Fatalf("Load err = %v, want duplicate normalized flag name", err)
	}
}

func TestOperationConflictGroupsLoad(t *testing.T) {
	raw := []byte(`
schema_version: 2
product: ecs
resource: disk
kind: regional
schema:
  fields:
    id:
      type: string
    name:
      type: string
    encryption_default_action:
      type: string
operations:
  update:
    input:
      fields: [id, name, encryption_default_action]
    conflicts:
      - any: [id, name]
        with_any: [encryption_default_action]
    workflow: []
`)

	loaded, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	conflicts := loaded.Operations["update"].Conflicts
	if len(conflicts) != 1 || !hasString(conflicts[0].Any, "name") || !hasString(conflicts[0].WithAny, "encryption_default_action") {
		t.Fatalf("operation conflicts = %#v", conflicts)
	}
}

func TestOperationRequireWhenLoads(t *testing.T) {
	raw := []byte(`
schema_version: 2
product: ecs
resource: eni
kind: regional
schema:
  fields:
    id:
      type: string
    attach_instance_id:
      type: string
    network_card_index:
      type: integer
operations:
  update:
    input:
      fields: [id, attach_instance_id, network_card_index]
    require_when:
      - when_any: [network_card_index]
        require_any: [attach_instance_id]
    workflow: []
`)

	loaded, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	requirements := loaded.Operations["update"].RequireWhen
	if len(requirements) != 1 || !hasString(requirements[0].WhenAny, "network_card_index") || !hasString(requirements[0].RequireAny, "attach_instance_id") {
		t.Fatalf("operation require_when = %#v", requirements)
	}
}

func TestWorkflowStepConditionsLoad(t *testing.T) {
	raw := []byte(`
schema_version: 2
product: ecs
resource: sg
kind: regional
schema:
  fields:
    id:
      type: string
    direction:
      type: string
    rule_id:
      type: string
bindings:
  revoke_ingress:
    api: RevokeSecurityGroup
    request:
      SecurityGroupId: $.id
  revoke_egress:
    api: RevokeSecurityGroupEgress
    request:
      SecurityGroupId: $.id
operations:
  revoke:
    input:
      fields: [id, direction, rule_id]
    workflow:
      - binding: revoke_ingress
        unless: input.direction == egress
        when_any: [input.rule_id]
      - binding: revoke_egress
        when: input.direction == egress
        when_any: [input.rule_id]
`)

	loaded, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	workflow := loaded.Operations["revoke"].Workflow
	if workflow[0].Unless != "input.direction == egress" || len(workflow[0].WhenAny) != 1 || workflow[0].WhenAny[0] != "input.rule_id" {
		t.Fatalf("ingress workflow condition = %#v", workflow[0])
	}
	if workflow[1].When != "input.direction == egress" || len(workflow[1].WhenAny) != 1 || workflow[1].WhenAny[0] != "input.rule_id" {
		t.Fatalf("egress workflow condition = %#v", workflow[1])
	}
}

func TestLoadRejectsInvalidOperationFieldOptions(t *testing.T) {
	cases := []struct {
		name  string
		field string
	}{
		{name: "empty name", field: `- ""`},
		{name: "multiple keys", field: `- {type: {required: true}, image: {required: true}}`},
		{name: "required type", field: `- type: {required: yes}`},
		{name: "repeatable type", field: `- type: {repeatable: yes}`},
		{name: "positional type", field: `- type: {positional: yes}`},
		{name: "positional many type", field: `- type: {positional_many: yes}`},
		{name: "input style type", field: `- type: {input_style: true}`},
		{name: "enum type", field: `- type: {enum: tcp}`},
		{name: "enum entry type", field: `- type: {enum: [tcp, 1]}`},
		{name: "description type", field: `- type: {description: [bad]}`},
		{name: "description entry type", field: `- type: {description: {en: 1}}`},
		{name: "unknown option", field: `- type: {unknown: true}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := []byte(`
schema_version: 2
product: ecs
resource: instance
kind: regional
schema:
  fields:
    type:
      type: string
operations:
  create:
    input:
      fields:
        ` + tc.field + `
`)
			if _, err := Load(raw); err == nil {
				t.Fatal("Load succeeded, want error")
			}
		})
	}
}

func TestValidateRejectsInvalidResourceSpecs(t *testing.T) {
	base := ResourceSpec{
		SchemaVersion: 2,
		Product:       "ecs",
		Resource:      "instance",
		Kind:          "regional",
		Schema:        ResourceSchema{Fields: map[string]SchemaField{"id": {Type: "string"}}},
	}
	cases := []struct {
		name string
		edit func(*ResourceSpec)
	}{
		{name: "missing schema version", edit: func(s *ResourceSpec) { s.SchemaVersion = 0 }},
		{name: "unsupported schema version", edit: func(s *ResourceSpec) { s.SchemaVersion = 1 }},
		{name: "missing product", edit: func(s *ResourceSpec) { s.Product = "" }},
		{name: "missing resource", edit: func(s *ResourceSpec) { s.Resource = "" }},
		{name: "missing kind", edit: func(s *ResourceSpec) { s.Kind = "" }},
		{name: "missing schema fields", edit: func(s *ResourceSpec) { s.Schema.Fields = nil }},
		{name: "scalar with items", edit: func(s *ResourceSpec) {
			s.Schema.Fields["id"] = SchemaField{Type: "string", Items: &SchemaField{Type: "string"}}
		}},
		{name: "array missing items", edit: func(s *ResourceSpec) { s.Schema.Fields["id"] = SchemaField{Type: "array"} }},
		{name: "object with items", edit: func(s *ResourceSpec) {
			s.Schema.Fields["id"] = SchemaField{Type: "object", Items: &SchemaField{Type: "string"}}
		}},
		{name: "unsupported field type", edit: func(s *ResourceSpec) { s.Schema.Fields["id"] = SchemaField{Type: "unsupported"} }},
		{name: "probe missing api", edit: func(s *ResourceSpec) {
			s.Probes = map[string]Probe{"state": {Response: ProbeResponse{Fields: map[string]ProbeField{"id": {Path: "$.Id"}}}}}
		}},
		{name: "probe item and items", edit: func(s *ResourceSpec) {
			s.Probes = map[string]Probe{"state": {API: "Describe", Response: ProbeResponse{Item: "$", Items: "$.Items"}}}
		}},
		{name: "probe field missing mapping", edit: func(s *ResourceSpec) {
			s.Probes = map[string]Probe{"state": {API: "Describe", Response: ProbeResponse{Fields: map[string]ProbeField{"id": {}}}}}
		}},
		{name: "probe each missing from", edit: func(s *ResourceSpec) {
			s.Probes = map[string]Probe{"state": {API: "Describe", Response: ProbeResponse{Fields: map[string]ProbeField{"tags": {Each: map[string]ProbeField{"key": {Path: "$.Key"}}}}}}}
		}},
		{name: "waiter missing probe", edit: func(s *ResourceSpec) { s.Waiters = map[string]Waiter{"ready": {Target: "Available"}} }},
		{name: "waiter unknown probe", edit: func(s *ResourceSpec) { s.Waiters = map[string]Waiter{"ready": {Probe: "state", Target: "Available"}} }},
		{name: "waiter missing target", edit: func(s *ResourceSpec) {
			s.Probes = map[string]Probe{"state": {API: "Describe"}}
			s.Waiters = map[string]Waiter{"ready": {Probe: "state"}}
		}},
		{name: "binding missing api", edit: func(s *ResourceSpec) { s.Bindings = map[string]Binding{"create": {Request: map[string]any{}}} }},
		{name: "binding missing request", edit: func(s *ResourceSpec) { s.Bindings = map[string]Binding{"create": {API: "Create"}} }},
		{name: "binding idempotency prefix without field", edit: func(s *ResourceSpec) {
			s.Bindings = map[string]Binding{"create": {API: "Create", Request: map[string]any{}, Idempotency: Idempotency{Prefix: "x"}}}
		}},
		{name: "binding idempotency field without prefix", edit: func(s *ResourceSpec) {
			s.Bindings = map[string]Binding{"create": {API: "Create", Request: map[string]any{}, Idempotency: Idempotency{Field: "ClientToken"}}}
		}},
		{name: "binding unsupported retry", edit: func(s *ResourceSpec) {
			s.Bindings = map[string]Binding{"create": {API: "Create", Request: map[string]any{}, Retry: TransitionRetry{Policy: "bad"}}}
		}},
		{name: "binding unknown waiter", edit: func(s *ResourceSpec) {
			s.Bindings = map[string]Binding{"create": {API: "Create", Request: map[string]any{}, Wait: "ready"}}
		}},
		{name: "binding empty require any", edit: func(s *ResourceSpec) {
			s.Bindings = map[string]Binding{"create": {API: "Create", Request: map[string]any{}, RequireAny: []Requirement{{}}}}
		}},
		{name: "operation unknown probe", edit: func(s *ResourceSpec) {
			s.Operations = map[string]Operation{"get": {Call: OperationCall{Probe: "state"}}}
		}},
		{name: "operation unknown field", edit: func(s *ResourceSpec) {
			s.Operations = map[string]Operation{"get": {Input: OperationInput{Fields: OperationFields{{Name: "missing"}}}}}
		}},
		{name: "operation unknown control", edit: func(s *ResourceSpec) {
			s.Operations = map[string]Operation{"get": {Input: OperationInput{Controls: OperationFields{{Name: "missing"}}}}}
		}},
		{name: "operation filter missing target", edit: func(s *ResourceSpec) {
			s.Operations = map[string]Operation{"list": {Filters: map[string]Filter{"name": {}}}}
		}},
		{name: "operation filter unknown input", edit: func(s *ResourceSpec) {
			s.Operations = map[string]Operation{"list": {Filters: map[string]Filter{"name": {Target: "missing"}}}}
		}},
		{name: "operation unknown binding", edit: func(s *ResourceSpec) {
			s.Operations = map[string]Operation{"create": {Workflow: []WorkflowStep{{Binding: "create"}}}}
		}},
		{name: "operation unknown workflow probe", edit: func(s *ResourceSpec) {
			s.Operations = map[string]Operation{"get": {Workflow: []WorkflowStep{{Probe: "state"}}}}
		}},
		{name: "output select missing from", edit: func(s *ResourceSpec) {
			s.Operations = map[string]Operation{"get": {Output: OperationOutput{Select: []OutputSelect{{SingleKey: "item"}}}}}
		}},
		{name: "output select missing key", edit: func(s *ResourceSpec) {
			s.Operations = map[string]Operation{"get": {Output: OperationOutput{Select: []OutputSelect{{From: "captures"}}}}}
		}},
		{name: "output select match missing by", edit: func(s *ResourceSpec) {
			s.Operations = map[string]Operation{"get": {Output: OperationOutput{Select: []OutputSelect{{From: "captures", SingleKey: "item", Match: "id"}}}}}
		}},
		// Mutating ops (P0.2 acceptance) must always declare ≥1 example so
		// agents can discover concrete invocations via `ecctl examples`.
		{name: "mutating create operation without examples", edit: func(s *ResourceSpec) {
			s.Operations = map[string]Operation{"create": {}}
		}},
		{name: "mutating attach operation without examples", edit: func(s *ResourceSpec) {
			s.Operations = map[string]Operation{"attach": {}}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := base
			spec.Schema.Fields = map[string]SchemaField{"id": {Type: "string"}}
			tc.edit(&spec)
			if err := Validate(spec); err == nil {
				t.Fatal("Validate succeeded, want error")
			}
		})
	}
}

func TestValidateProductRejectsInvalidSpecs(t *testing.T) {
	valid := ProductSpec{
		SchemaVersion: 1,
		Product:       "ecs",
		Description:   LocalizedText{"en": "Elastic Compute Service"},
		Examples:      []string{"ecctl ecs instance list", "ecctl ecs sg list"},
	}
	cases := []struct {
		name string
		edit func(*ProductSpec)
	}{
		{name: "missing schema version", edit: func(s *ProductSpec) { s.SchemaVersion = 0 }},
		{name: "missing product", edit: func(s *ProductSpec) { s.Product = "" }},
		{name: "missing description", edit: func(s *ProductSpec) { s.Description = nil }},
		{name: "too few examples", edit: func(s *ProductSpec) { s.Examples = []string{"one"} }},
		{name: "too many examples", edit: func(s *ProductSpec) { s.Examples = []string{"1", "2", "3", "4", "5"} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := valid
			tc.edit(&spec)
			if err := ValidateProduct(spec); err == nil {
				t.Fatal("ValidateProduct succeeded, want error")
			}
		})
	}
}

func TestValidateSchemaPathsRejectsNestedResourceActionCollision(t *testing.T) {
	resources := []ResourceSpec{
		{
			Product:    "demo",
			Resource:   "alpha",
			Operations: map[string]Operation{"item": {}},
		},
		{
			Product:  "demo",
			Parent:   "alpha",
			Resource: "item",
		},
	}

	err := ValidateSchemaPaths(resources)
	if err == nil || !strings.Contains(err.Error(), "demo.alpha.item") {
		t.Fatalf("ValidateSchemaPaths error = %v, want colliding schema ID", err)
	}
}

func TestLocalizedTextRejectsUnsupportedYAMLShape(t *testing.T) {
	raw := []byte(`
schema_version: 1
product: ecs
resource: instance
kind: regional
display_name: [bad]
`)

	if _, err := Load(raw); err == nil {
		t.Fatal("Load succeeded with invalid localized text")
	}
}

func TestLocalizedTextFallbacksToSortedFirstValue(t *testing.T) {
	text := LocalizedText{"fr": "French", "de": "German"}

	if got := text.Text("es-MX"); got != "German" {
		t.Fatalf("Text(es-MX) = %q, want sorted fallback", got)
	}
	if got := (LocalizedText{}).String(); got != "" {
		t.Fatalf("empty String() = %q, want empty", got)
	}
}

func TestLoadVPCResourceLifecycleSpec(t *testing.T) {
	loaded, err := LoadFile("../../specs/vpc/vpc.yaml")
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if loaded.Product != "vpc" || loaded.Resource != "vpc" {
		t.Fatalf("loaded wrong resource: %#v", loaded)
	}
	if loaded.Probes["list"].API != "DescribeVpcs" {
		t.Fatalf("list probe API = %q", loaded.Probes["list"].API)
	}
	if loaded.Probes["attribute"].API != "DescribeVpcAttribute" {
		t.Fatalf("attribute probe API = %q", loaded.Probes["attribute"].API)
	}
	if loaded.Operations["get"].Workflow[0].Probe != "attribute" {
		t.Fatalf("get probe = %q, want attribute", loaded.Operations["get"].Workflow[0].Probe)
	}
	list := loaded.Operations["list"]
	if list.Filters["owner-id"].Target != "owner_id" {
		t.Fatalf("owner-id filter target = %q, want owner_id", list.Filters["owner-id"].Target)
	}
	if list.Filters["tag."].KeyPrefix != "tag." || list.Filters["tag."].Target != "tag" {
		t.Fatalf("tag filter = %#v, want tag prefix mapping", list.Filters["tag."])
	}
	create := loaded.Bindings["create_to_available"]
	if create.API != "CreateVpc" || create.Idempotency.Field != "ClientToken" {
		t.Fatalf("create binding = %#v", create)
	}
	update := loaded.Bindings["update_attributes"]
	if update.API != "ModifyVpcAttribute" || update.Idempotency.Field != "" {
		t.Fatalf("update binding = %#v", update)
	}
	if loaded.Waiters["available_after_create"].Probe != "attribute" {
		t.Fatalf("create waiter probe = %q", loaded.Waiters["available_after_create"].Probe)
	}
	for _, name := range []string{
		"description",
		"resource_group",
		"enable_ipv6",
		"ipv6_cidr",
		"user_cidr",
		"ipv6_isp",
		"ipv4_ipam_pool",
		"ipv4_cidr_mask",
		"enable_dns_hostname",
		"ipv6_ipam_pool",
		"ipv6_cidr_mask",
		"default",
		"owner_id",
		"dhcp_options_set",
	} {
		if _, ok := loaded.Schema.Fields[name]; !ok {
			t.Fatalf("VPC schema field %q missing", name)
		}
	}
	if err := Validate(loaded); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestLoadVSwitchResourceLifecycleSpec(t *testing.T) {
	loaded, err := LoadFile("../../specs/vpc/vswitch.yaml")
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if loaded.Product != "vpc" || loaded.Resource != "vswitch" {
		t.Fatalf("loaded wrong resource: %#v", loaded)
	}
	if len(loaded.Aliases) != 1 || loaded.Aliases[0] != "vsw" {
		t.Fatalf("aliases = %#v, want [vsw]", loaded.Aliases)
	}
	if loaded.Identity.OutputRoot.One != "vswitch" || loaded.Identity.OutputRoot.Many != "vswitches" {
		t.Fatalf("output roots = %#v", loaded.Identity.OutputRoot)
	}
	if loaded.Probes["list"].API != "DescribeVSwitches" {
		t.Fatalf("list probe API = %q", loaded.Probes["list"].API)
	}
	if loaded.Probes["attribute"].API != "DescribeVSwitchAttributes" {
		t.Fatalf("attribute probe API = %q", loaded.Probes["attribute"].API)
	}
	if loaded.Bindings["create_to_available"].API != "CreateVSwitch" {
		t.Fatalf("create binding = %#v", loaded.Bindings["create_to_available"])
	}
	if loaded.Bindings["update_attributes"].API != "ModifyVSwitchAttribute" {
		t.Fatalf("update binding = %#v", loaded.Bindings["update_attributes"])
	}
	if loaded.Bindings["delete_to_absent"].API != "DeleteVSwitch" {
		t.Fatalf("delete binding = %#v", loaded.Bindings["delete_to_absent"])
	}
	if loaded.Operations["list"].Filters["vpc"].Target != "vpc" {
		t.Fatalf("vpc filter = %#v", loaded.Operations["list"].Filters["vpc"])
	}
	for _, name := range []string{"vpc", "zone", "cidr"} {
		if !operationFieldRequired(loaded.Operations["create"].Input.Fields, name) {
			t.Fatalf("create param %q should be required", name)
		}
	}
	if err := Validate(loaded); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestLoadECSSecurityGroupLifecycleSpec(t *testing.T) {
	loaded, err := LoadFile("../../specs/ecs/sg.yaml")
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if loaded.Product != "ecs" || loaded.Resource != "sg" {
		t.Fatalf("loaded wrong resource: %#v", loaded)
	}
	if loaded.Identity.OutputRoot.One != "security_group" || loaded.Identity.OutputRoot.Many != "security_groups" {
		t.Fatalf("output roots = %#v, want security_group/security_groups", loaded.Identity.OutputRoot)
	}
	if loaded.Probes["list"].API != "DescribeSecurityGroups" {
		t.Fatalf("list probe API = %q", loaded.Probes["list"].API)
	}
	if loaded.Probes["attribute"].API != "DescribeSecurityGroupAttribute" {
		t.Fatalf("attribute probe API = %q", loaded.Probes["attribute"].API)
	}
	if loaded.Probes["references"].API != "DescribeSecurityGroupReferences" {
		t.Fatalf("references probe API = %q", loaded.Probes["references"].API)
	}
	if loaded.Operations["get"].Workflow[0].Probe != "attribute" {
		t.Fatalf("get probe = %q, want attribute", loaded.Operations["get"].Workflow[0].Probe)
	}
	if len(loaded.Operations["get"].Workflow) < 2 || loaded.Operations["get"].Workflow[1].Probe != "references" || loaded.Operations["get"].Workflow[1].When != "input.with_references" {
		t.Fatalf("get references workflow = %#v", loaded.Operations["get"].Workflow)
	}
	list := loaded.Operations["list"]
	if !operationFieldPositionalMany(list.Input.Fields, "ids") || list.Filters["vpc"].Target != "vpc" || list.Filters["tag."].Target != "tag" {
		t.Fatalf("list ids/filter config = %#v %#v", list.Input.Fields, list.Filters)
	}
	create := loaded.Bindings["create_to_available"]
	if create.API != "CreateSecurityGroup" || create.Idempotency.Field != "ClientToken" {
		t.Fatalf("create binding = %#v", create)
	}
	if loaded.Bindings["update_attributes"].API != "ModifySecurityGroupAttribute" {
		t.Fatalf("update attributes binding = %#v", loaded.Bindings["update_attributes"])
	}
	if loaded.Bindings["update_policy"].API != "ModifySecurityGroupPolicy" {
		t.Fatalf("update policy binding = %#v", loaded.Bindings["update_policy"])
	}
	if loaded.Bindings["update_rule"].API != "ModifySecurityGroupRule" || loaded.Bindings["update_rule_egress"].API != "ModifySecurityGroupEgressRule" {
		t.Fatalf("update rule bindings = %#v %#v", loaded.Bindings["update_rule"], loaded.Bindings["update_rule_egress"])
	}
	deleteBinding := loaded.Bindings["delete_to_absent"]
	if deleteBinding.API != "DeleteSecurityGroup" {
		t.Fatalf("delete binding = %#v", deleteBinding)
	}
	authorizeBinding := loaded.Bindings["authorize_rules"]
	if authorizeBinding.API != "AuthorizeSecurityGroup" {
		t.Fatalf("authorize binding = %#v", authorizeBinding)
	}
	permissions, ok := authorizeBinding.Request["Permissions"].(map[string]any)
	if !ok || permissions["capture"] != "rule_permissions" {
		t.Fatalf("authorize permissions = %#v", authorizeBinding.Request["Permissions"])
	}
	if loaded.Bindings["authorize_rules_egress"].API != "AuthorizeSecurityGroupEgress" {
		t.Fatalf("authorize egress binding = %#v", loaded.Bindings["authorize_rules_egress"])
	}
	revokeBinding := loaded.Bindings["revoke_rules"]
	if revokeBinding.API != "RevokeSecurityGroup" {
		t.Fatalf("revoke binding = %#v", revokeBinding)
	}
	if loaded.Bindings["revoke_rules_egress"].API != "RevokeSecurityGroupEgress" {
		t.Fatalf("revoke egress binding = %#v", loaded.Bindings["revoke_rules_egress"])
	}
	ruleID, ok := revokeBinding.Request["SecurityGroupRuleId"].(map[string]any)
	if !ok || ruleID["each"] != "$.rule_id" {
		t.Fatalf("revoke rule id mapping = %#v", revokeBinding.Request["SecurityGroupRuleId"])
	}
	if !operationFieldRequired(loaded.Operations["create"].Input.Fields, "vpc") {
		t.Fatalf("create --vpc should be required: %#v", loaded.Operations["create"].Input.Fields)
	}
	if err := Validate(loaded); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestLoadECSDiskResourceLifecycleSpec(t *testing.T) {
	loaded, err := LoadFile("../../specs/ecs/disk.yaml")
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if loaded.Product != "ecs" || loaded.Resource != "disk" {
		t.Fatalf("loaded wrong resource: %#v", loaded)
	}
	if loaded.Identity.OutputRoot.One != "disk" || loaded.Identity.OutputRoot.Many != "disks" {
		t.Fatalf("output roots = %#v, want disk/disks", loaded.Identity.OutputRoot)
	}
	if loaded.Probes["list"].API != "DescribeDisks" {
		t.Fatalf("list probe API = %q, want DescribeDisks", loaded.Probes["list"].API)
	}
	for _, action := range []string{"create", "update", "delete", "get", "list", "attach", "detach", "clone", "monitor", "reinit", "reset"} {
		if _, ok := loaded.Operations[action]; !ok {
			t.Fatalf("disk operation %q missing", action)
		}
	}
	for name, api := range map[string]string{
		"create_to_available":        "CreateDisk",
		"update_attributes":          "ModifyDiskAttribute",
		"resize":                     "ResizeDisk",
		"modify_spec":                "ModifyDiskSpec",
		"modify_charge_type":         "ModifyDiskChargeType",
		"enable_encryption_default":  "EnableDiskEncryptionByDefault",
		"disable_encryption_default": "DisableDiskEncryptionByDefault",
		"modify_default_kms_key":     "ModifyDiskDefaultKMSKeyId",
		"reset_default_kms_key":      "ResetDiskDefaultKMSKeyId",
		"delete_to_absent":           "DeleteDisk",
		"attach_to_instance":         "AttachDisk",
		"detach_from_instance":       "DetachDisk",
		"clone_to_available":         "CloneDisks",
		"reinit_to_target":           "ReInitDisk",
		"reset_to_target":            "ResetDisk",
	} {
		if loaded.Bindings[name].API != api {
			t.Fatalf("binding %s API = %q, want %q", name, loaded.Bindings[name].API, api)
		}
	}
	if !operationFieldPresent(loaded.Operations["create"].Input.Fields, "zone") {
		t.Fatalf("disk create --zone input missing: %#v", loaded.Operations["create"].Input.Fields)
	}
	if loaded.Controls["force"].Default != false {
		t.Fatalf("disk delete --force default = %#v, want false", loaded.Controls["force"].Default)
	}
	if loaded.Bindings["delete_to_absent"].Retry.Policy == "" || !hasString(loaded.Bindings["delete_to_absent"].Retry.Errors, "IncorrectDiskStatus.Initializing") {
		t.Fatalf("disk delete retry config = %#v", loaded.Bindings["delete_to_absent"].Retry)
	}
	if err := Validate(loaded); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestLoadECSInstanceResourceLifecycleSpec(t *testing.T) {
	loaded, err := LoadFile("../../specs/ecs/instance.yaml")
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if loaded.Product != "ecs" || loaded.Resource != "instance" {
		t.Fatalf("loaded wrong resource: %#v", loaded)
	}
	if !hasLocalizedText(loaded.DisplayName) {
		t.Fatalf("display metadata must include en and zh-CN: %#v", loaded.DisplayName)
	}
	if !hasLocalizedText(loaded.Description) {
		t.Fatalf("description metadata must include en and zh-CN: %#v", loaded.Description)
	}
	if !hasLocalizedText(loaded.Schema.Fields["image"].Description) {
		t.Fatalf("image description must include en and zh-CN: %#v", loaded.Schema.Fields["image"].Description)
	}
	if !hasLocalizedText(loaded.Operations["create"].Description) {
		t.Fatalf("create description must include en and zh-CN: %#v", loaded.Operations["create"].Description)
	}
	requireLocalizedText(t, "messages.ImageNotFound", loaded.Messages["ImageNotFound"])
	if got := i18n.NewLocalizer("zh-CN").Message("ImageNotFound"); got != "镜像不存在" {
		t.Fatalf("ImageNotFound localization = %q, want spec zh message", got)
	}
	if loaded.Probes["state"].API != "DescribeInstances" {
		t.Fatalf("state probe API = %q, want DescribeInstances", loaded.Probes["state"].API)
	}
	create := loaded.Bindings["create_to_running"]
	if create.API != "RunInstances" || create.IDFrom != "$.InstanceIdSets.InstanceIdSet" || create.Idempotency.Field != "ClientToken" {
		t.Fatalf("create binding = %#v", create)
	}
	if loaded.Bindings["update_attributes"].API != "ModifyInstanceAttribute" {
		t.Fatalf("update binding = %#v", loaded.Bindings["update_attributes"])
	}
	deleteBinding := loaded.Bindings["delete_to_absent"]
	if deleteBinding.API != "DeleteInstance" {
		t.Fatalf("delete binding = %#v", deleteBinding)
	}
	if deleteBinding.Retry.Policy != "initializing_grace" || len(deleteBinding.Retry.Errors) != 1 || deleteBinding.Retry.Errors[0] != "IncorrectInstanceStatus" {
		t.Fatalf("delete retry = %#v", deleteBinding.Retry)
	}
	if deleteBinding.Retry.InitialInterval != "5s" || deleteBinding.Retry.MaxInterval != "10s" || deleteBinding.Retry.Timeout != "120s" {
		t.Fatalf("delete retry timing = %#v", deleteBinding.Retry)
	}
	if loaded.Controls["force"].Default != false {
		t.Fatalf("delete --force default = %#v, want false (safe default; force must be opt-in)", loaded.Controls["force"].Default)
	}
	if !hasString(loaded.Schema.Fields["status"].Enum, "Running") {
		t.Fatalf("instance status enum should contain raw API value Running: %#v", loaded.Schema.Fields["status"].Enum)
	}
	if loaded.Waiters["running_after_create"].Target != "Running" {
		t.Fatalf("running waiter target = %q, want raw Running", loaded.Waiters["running_after_create"].Target)
	}
	if loaded.Probes["attribute"].API != "DescribeInstanceAttribute" {
		t.Fatalf("attribute probe API = %q, want DescribeInstanceAttribute", loaded.Probes["attribute"].API)
	}
	if loaded.Operations["get"].Workflow[0].Probe != "attribute" {
		t.Fatalf("get probe = %q, want attribute", loaded.Operations["get"].Workflow[0].Probe)
	}
	if loaded.Operations["list"].Filters["tag."].KeyPrefix != "tag." {
		t.Fatalf("tag filter = %#v, want tag prefix mapping", loaded.Operations["list"].Filters["tag."])
	}
	for _, name := range []string{"image", "type", "sg", "vswitch", "status"} {
		if _, ok := loaded.Schema.Fields[name]; !ok {
			t.Fatalf("ECS instance schema field %q missing", name)
		}
	}
	if _, ok := loaded.Controls["force"]; !ok {
		t.Fatal("ECS instance force control missing")
	}
	if err := Validate(loaded); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestECSInstanceCreateCoversRunInstancesParameters(t *testing.T) {
	loaded, err := LoadFile("../../specs/ecs/instance.yaml")
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	create := loaded.Bindings["create_to_running"]
	covered := bindingRequestCoverage(create.Request)
	for _, name := range []string{
		"Affinity", "Amount", "Arn.AssumeRoleFor", "Arn.RoleType", "Arn.Rolearn", "AutoPay", "AutoReleaseTime", "AutoRenew", "AutoRenewPeriod",
		"ClockOptions", "CpuOptions.Core", "CpuOptions.Numa", "CpuOptions.ThreadsPerCore", "CpuOptions.TopologyType",
		"CreditSpecification", "DataDisk.AutoSnapshotPolicyId", "DataDisk.BurstingEnabled", "DataDisk.Category",
		"DataDisk.DeleteWithInstance", "DataDisk.Description", "DataDisk.Device", "DataDisk.DiskName",
		"DataDisk.EncryptAlgorithm", "DataDisk.Encrypted", "DataDisk.KMSKeyId", "DataDisk.PerformanceLevel",
		"DataDisk.ProvisionedIops", "DataDisk.Size", "DataDisk.SnapshotId", "DataDisk.StorageClusterId",
		"DedicatedHostId", "DeletionProtection", "DeploymentSetGroupNo", "DeploymentSetId", "Description", "DryRun",
		"HibernationOptions.Configured", "HostName", "HostNames", "HpcClusterId", "HttpEndpoint",
		"HttpPutResponseHopLimit", "HttpTokens", "ImageFamily", "ImageId", "ImageOptions", "InstanceChargeType",
		"InstanceName", "InstanceType", "InternetChargeType", "InternetMaxBandwidthIn", "InternetMaxBandwidthOut",
		"IoOptimized", "Ipv6Address", "Ipv6AddressCount", "Isp", "KeyPairName", "LaunchTemplateId",
		"LaunchTemplateName", "LaunchTemplateVersion", "MinAmount", "NetworkInterface.DeleteOnRelease",
		"NetworkInterface.Description", "NetworkInterface.InstanceType", "NetworkInterface.Ipv6Address",
		"NetworkInterface.Ipv6AddressCount", "NetworkInterface.NetworkCardIndex", "NetworkInterface.NetworkInterfaceId",
		"NetworkInterface.NetworkInterfaceName", "NetworkInterface.NetworkInterfaceTrafficMode",
		"NetworkInterface.PrimaryIpAddress", "NetworkInterface.QueueNumber", "NetworkInterface.QueuePairNumber",
		"NetworkInterface.RxQueueSize", "NetworkInterface.SecondaryPrivateIpAddressCount", "NetworkInterface.SecurityGroupId",
		"NetworkInterface.SecurityGroupIds", "NetworkInterface.SourceDestCheck", "NetworkInterface.TxQueueSize",
		"NetworkInterface.VSwitchId", "NetworkInterfaceQueueNumber", "NetworkOptions", "Password", "PasswordInherit",
		"Period", "PeriodUnit", "PrivateDnsNameOptions", "PrivateIpAddress", "PrivatePoolOptions.Id",
		"PrivatePoolOptions.MatchCriteria", "RamRoleName", "ResourceGroupId", "SchedulerOptions.DedicatedHostClusterId",
		"SecurityEnhancementStrategy", "SecurityGroupId", "SecurityGroupIds",
		"SecurityOptions.ConfidentialComputingMode", "SecurityOptions.TrustedSystemMode", "SpotDuration",
		"SpotInterruptionBehavior", "SpotPriceLimit", "SpotStrategy", "StorageSetId", "StorageSetPartitionNumber",
		"SystemDisk.AutoSnapshotPolicyId", "SystemDisk.Category", "SystemDisk.Description", "SystemDisk.DiskName",
		"SystemDisk.PerformanceLevel", "SystemDisk.Size", "Tag", "Tenancy", "UniqueSuffix", "UserData", "VSwitchId", "ZoneId",
	} {
		if !covered[name] {
			t.Fatalf("RunInstances parameter %s is not covered by ecs instance create", name)
		}
	}
}

func TestBuiltInListAndGetProbesCoverQueryResponseResourceFields(t *testing.T) {
	tests := []struct {
		name     string
		product  string
		resource string
		probe    string
		paths    []string
		fields   map[string]string
	}{
		{
			name:     "vpc list",
			product:  "vpc",
			resource: "vpc",
			probe:    "list",
			paths: []string{
				"$.CenStatus", "$.CidrBlock", "$.CreationTime", "$.Description",
				"$.DhcpOptionsSetId", "$.DhcpOptionsSetStatus", "$.DnsHostnameStatus",
				"$.EnabledIpv6", "$.Ipv6CidrBlock", "$.Ipv6CidrBlocks.Ipv6CidrBlock",
				"$.IsDefault", "$.NatGatewayIds.NatGatewayIds", "$.OwnerId",
				"$.RegionId", "$.ResourceGroupId", "$.RouterTableIds.RouterTableIds",
				"$.SecondaryCidrBlocks.SecondaryCidrBlock", "$.Status", "$.Tags.Tag",
				"$.UserCidrs.UserCidr", "$.VRouterId", "$.VSwitchIds.VSwitchId",
				"$.VpcId", "$.VpcName",
			},
		},
		{
			name:     "vpc get",
			product:  "vpc",
			resource: "vpc",
			probe:    "attribute",
			paths: []string{
				"$.AssociatedCens.AssociatedCen", "$.AssociatedPropagationSources.AssociatedPropagationSources",
				"$.CidrBlock", "$.ClassicLinkEnabled", "$.CloudResources.CloudResourceSetType",
				"$.CreationTime", "$.Description", "$.DhcpOptionsSetId",
				"$.DhcpOptionsSetStatus", "$.DnsHostnameStatus", "$.EnabledIpv6",
				"$.Ipv4GatewayId", "$.Ipv6CidrBlock", "$.Ipv6CidrBlocks.Ipv6CidrBlock",
				"$.IsDefault", "$.OwnerId", "$.RegionId", "$.ResourceGroupId",
				"$.SecondaryCidrBlocks.SecondaryCidrBlock", "$.Status", "$.SupportIpv4Gateway",
				"$.Tags.Tag", "$.UserCidrs.UserCidr", "$.VRouterId", "$.VSwitchIds.VSwitchId",
				"$.VpcId", "$.VpcName",
			},
		},
		{
			name:     "vswitch list",
			product:  "vpc",
			resource: "vswitch",
			probe:    "list",
			paths: []string{
				"$.AvailableIpAddressCount", "$.CidrBlock", "$.CreationTime", "$.Description",
				"$.EnabledIpv6", "$.Ipv6CidrBlock", "$.IsDefault", "$.NetworkAclId",
				"$.OwnerId", "$.ResourceGroupId", "$.RouteTable.RouteTableId",
				"$.RouteTable.RouteTableType", "$.ShareType", "$.Status", "$.Tags.Tag",
				"$.VSwitchId", "$.VSwitchName", "$.VpcId", "$.ZoneId",
			},
		},
		{
			name:     "vswitch get",
			product:  "vpc",
			resource: "vswitch",
			probe:    "attribute",
			paths: []string{
				"$.AvailableIpAddressCount", "$.CidrBlock", "$.CreationTime", "$.Description",
				"$.EnabledIpv6", "$.Ipv6CidrBlock", "$.IsDefault", "$.NetworkAclId",
				"$.OwnerId", "$.ResourceGroupId", "$.RouteTable.RouteTableId",
				"$.RouteTable.RouteTableType", "$.ShareType", "$.Status", "$.Tags.Tag",
				"$.VSwitchId", "$.VSwitchName", "$.VpcId", "$.ZoneId",
			},
		},
		{
			name:     "security group list",
			product:  "ecs",
			resource: "sg",
			probe:    "list",
			paths: []string{
				"$.AvailableInstanceAmount", "$.CreationTime", "$.Description", "$.EcsCount",
				"$.GroupToGroupRuleCount", "$.ResourceGroupId", "$.RuleCount",
				"$.SecurityGroupId", "$.SecurityGroupName", "$.SecurityGroupType",
				"$.ServiceID", "$.ServiceManaged", "$.Tags.Tag", "$.VpcId",
			},
		},
		{
			name:     "security group get",
			product:  "ecs",
			resource: "sg",
			probe:    "attribute",
			paths: []string{
				"$.Description", "$.InnerAccessPolicy", "$.NextToken", "$.Permissions.Permission",
				"$.RegionId", "$.SecurityGroupId", "$.SecurityGroupName",
				"$.SnapshotPolicyIds.SnapshotPolicyId", "$.VpcId",
			},
			fields: map[string]string{
				"permissions": "$.Permissions.Permission",
			},
		},
		{
			name:     "ecs instance list and get",
			product:  "ecs",
			resource: "instance",
			probe:    "state",
			paths: []string{
				"$.AdditionalInfo", "$.AutoReleaseTime", "$.ClockOptions", "$.ClusterId",
				"$.Cpu", "$.CpuOptions", "$.CreationTime", "$.CreditSpecification",
				"$.DedicatedHostAttribute", "$.DedicatedInstanceAttribute", "$.DeletionProtection",
				"$.DeploymentSetGroupNo", "$.DeploymentSetId", "$.Description",
				"$.DeviceAvailable", "$.EcsCapacityReservationAttr", "$.EipAddress",
				"$.EnableNVS", "$.ExpiredTime", "$.GPUAmount", "$.GPUSpec",
				"$.HibernationOptions", "$.HostName", "$.HpcClusterId", "$.ISP",
				"$.ImageId", "$.ImageOptions", "$.InnerIpAddress.IpAddress", "$.InstanceChargeType",
				"$.InstanceId", "$.InstanceName", "$.InstanceNetworkType", "$.InstanceType",
				"$.InstanceTypeFamily", "$.InternetChargeType", "$.InternetMaxBandwidthIn",
				"$.InternetMaxBandwidthOut", "$.IoOptimized", "$.KeyPairName",
				"$.LocalStorageAmount", "$.LocalStorageCapacity", "$.Memory",
				"$.MetadataOptions", "$.NetworkInterfaces.NetworkInterface", "$.OSName", "$.OSNameEn",
				"$.OSType", "$.OperationLocks.LockReason", "$.PrivateDnsNameOptions",
				"$.PublicIpAddress.IpAddress", "$.RdmaIpAddress.IpAddress", "$.Recyclable", "$.RegionId",
				"$.ResourceGroupId", "$.SaleCycle", "$.SecurityGroupIds.SecurityGroupId", "$.SerialNumber",
				"$.SpotDuration", "$.SpotInterruptionBehavior", "$.SpotPriceLimit",
				"$.SpotStrategy", "$.StartTime", "$.Status", "$.StoppedMode",
				"$.Tags.Tag", "$.VlanId", "$.VpcAttributes", "$.ZoneId",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loaded, err := LoadFile(filepath.Join("../../specs", tt.product, tt.resource+".yaml"))
			if err != nil {
				t.Fatalf("LoadFile: %v", err)
			}
			probe := loaded.Probes[tt.probe]
			requireProbeFieldPaths(t, probe, tt.paths)
			for field, path := range tt.fields {
				if got := probe.Response.Fields[field].Path; got != path {
					t.Fatalf("field %q = %q, want %q", field, got, path)
				}
			}
		})
	}
}

func TestBuiltInSpecsHaveBilingualDisplayText(t *testing.T) {
	for _, resource := range []struct {
		product string
		name    string
	}{
		{product: "ecs", name: "assistant"},
		{product: "ecs", name: "auto-snapshot-policy"},
		{product: "ecs", name: "command"},
		{product: "ecs", name: "disk"},
		{product: "ecs", name: "image"},
		{product: "ecs", name: "instance"},
		{product: "ecs", name: "keypair"},
		{product: "ecs", name: "region"},
		{product: "ecs", name: "sg"},
		{product: "ecs", name: "snapshot"},
		{product: "ecs", name: "snapshot-group"},
		{product: "ecs", name: "zone"},
		{product: "rg", name: "group"},
		{product: "tag", name: "associated-resource-rule"},
		{product: "tag", name: "policy"},
		{product: "tag", name: "resource"},
		{product: "vpc", name: "vpc"},
		{product: "vpc", name: "vswitch"},
	} {
		t.Run(resource.product+"/"+resource.name, func(t *testing.T) {
			loaded, err := LoadFile(filepath.Join("../../specs", resource.product, resource.name+".yaml"))
			if err != nil {
				t.Fatalf("LoadFile: %v", err)
			}
			requireLocalizedText(t, "display_name", loaded.DisplayName)
			requireLocalizedText(t, "description", loaded.Description)
			requireKeyExamples(t, loaded.Examples)
			for name, message := range loaded.Messages {
				requireLocalizedText(t, "messages."+name, message)
			}
			for name, field := range loaded.Schema.Fields {
				requireSchemaFieldLocalizedText(t, "schema.fields."+name, field)
			}
			for name, control := range loaded.Controls {
				requireLocalizedText(t, "controls."+name+".description", control.Description)
			}
			for name, operation := range loaded.Operations {
				requireLocalizedText(t, "operations."+name+".description", operation.Description)
			}
		})
	}
}

func TestBuiltInListOperationsUseOnePaginationMode(t *testing.T) {
	for _, resource := range []struct {
		product string
		name    string
	}{
		{product: "ecs", name: "assistant"},
		{product: "ecs", name: "auto-snapshot-policy"},
		{product: "ecs", name: "command"},
		{product: "ecs", name: "disk"},
		{product: "ecs", name: "image"},
		{product: "ecs", name: "instance"},
		{product: "ecs", name: "keypair"},
		{product: "ecs", name: "region"},
		{product: "ecs", name: "sg"},
		{product: "ecs", name: "snapshot"},
		{product: "ecs", name: "snapshot-group"},
		{product: "ecs", name: "zone"},
		{product: "rg", name: "group"},
		{product: "tag", name: "associated-resource-rule"},
		{product: "tag", name: "policy"},
		{product: "tag", name: "resource"},
		{product: "vpc", name: "vpc"},
		{product: "vpc", name: "vswitch"},
	} {
		t.Run(resource.product+"/"+resource.name, func(t *testing.T) {
			loaded, err := LoadFile(filepath.Join("../../specs", resource.product, resource.name+".yaml"))
			if err != nil {
				t.Fatalf("LoadFile: %v", err)
			}
			for name, operation := range loaded.Operations {
				hasPage := operationFieldPresent(operation.Input.Controls, "page")
				hasNextToken := operationFieldPresent(operation.Input.Controls, "next_token")
				if hasPage && hasNextToken {
					t.Fatalf("operation %q declares both page and next_token controls", name)
				}
			}
		})
	}
}

func TestECSListOperationsPreferTokenPaginationWhenSupported(t *testing.T) {
	for _, tt := range []struct {
		resource string
		probes   []string
		limitMax int
	}{
		{resource: "command", probes: []string{"state", "invocation", "invocation_result"}, limitMax: 50},
		{resource: "disk", probes: []string{"list", "list_by_id"}, limitMax: 500},
		{resource: "eni", probes: []string{"list"}, limitMax: 500},
		{resource: "instance", probes: []string{"state"}, limitMax: 100},
		{resource: "sg", probes: []string{"list"}, limitMax: 100},
		{resource: "snapshot", probes: []string{"list", "destination_list", "links"}, limitMax: 100},
	} {
		t.Run(tt.resource, func(t *testing.T) {
			loaded, err := LoadFile(filepath.Join("../../specs/ecs", tt.resource+".yaml"))
			if err != nil {
				t.Fatalf("LoadFile: %v", err)
			}

			listOp := loaded.Operations["list"]
			if !operationFieldPresent(listOp.Input.Controls, "next_token") || operationFieldPresent(listOp.Input.Controls, "page") {
				t.Fatalf("ecs %s list must use next_token pagination, controls=%#v", tt.resource, listOp.Input.Controls)
			}
			limit := loaded.Controls["limit"]
			if limit.Max == nil || *limit.Max != tt.limitMax {
				t.Fatalf("ecs %s limit max = %#v, want %d", tt.resource, limit.Max, tt.limitMax)
			}

			for _, probeName := range tt.probes {
				probe := loaded.Probes[probeName]
				if probe.Request["MaxResults"] != "$.limit" || probe.Request["NextToken"] != "$.next_token" {
					t.Fatalf("ecs %s probe %s token request = %#v", tt.resource, probeName, probe.Request)
				}
				if _, ok := probe.Request["PageNumber"]; ok {
					t.Fatalf("ecs %s probe %s still sends PageNumber: %#v", tt.resource, probeName, probe.Request)
				}
				if _, ok := probe.Request["PageSize"]; ok {
					t.Fatalf("ecs %s probe %s still sends PageSize: %#v", tt.resource, probeName, probe.Request)
				}
				if probe.Response.NextToken != "$.NextToken" {
					t.Fatalf("ecs %s probe %s next token response = %q", tt.resource, probeName, probe.Response.NextToken)
				}
				if probe.Response.Total != "" {
					t.Fatalf("ecs %s probe %s must not expose meaningless token total: %q", tt.resource, probeName, probe.Response.Total)
				}
			}
		})
	}
}

func TestECSAuxiliaryProbeUsesTokenPaginationWhenSupported(t *testing.T) {
	loaded, err := LoadFile(filepath.Join("../../specs/ecs", "auto-snapshot-policy.yaml"))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	probe := loaded.Probes["associations"]
	if probe.Request["MaxResults"] != "$.limit" || probe.Request["NextToken"] != "$.next_token" {
		t.Fatalf("auto snapshot policy associations token request = %#v", probe.Request)
	}
	if _, ok := probe.Request["PageNumber"]; ok {
		t.Fatalf("associations probe still sends PageNumber: %#v", probe.Request)
	}
	if _, ok := probe.Request["PageSize"]; ok {
		t.Fatalf("associations probe still sends PageSize: %#v", probe.Request)
	}
	if probe.Response.NextToken != "$.NextToken" || probe.Response.Total != "" {
		t.Fatalf("associations token response = %#v", probe.Response)
	}
}

func TestBuiltInProductSpecsHaveBilingualDescriptionsAndExamples(t *testing.T) {
	for _, product := range []string{"ecs", "rg", "tag", "vpc"} {
		t.Run(product, func(t *testing.T) {
			loaded, err := LoadProductFile(filepath.Join("../../specs", product, "product.yaml"))
			if err != nil {
				t.Fatalf("LoadProductFile: %v", err)
			}
			if err := ValidateProduct(loaded); err != nil {
				t.Fatalf("ValidateProduct: %v", err)
			}
			requireLocalizedText(t, "description", loaded.Description)
			requireKeyExamples(t, loaded.Examples)
		})
	}
}

func hasString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func operationFieldRequired(fields OperationFields, name string) bool {
	for _, field := range fields {
		if field.Name == name {
			return field.Required
		}
	}
	return false
}

func operationFieldPresent(fields OperationFields, name string) bool {
	for _, field := range fields {
		if field.Name == name {
			return true
		}
	}
	return false
}

func operationFieldPositionalMany(fields OperationFields, name string) bool {
	for _, field := range fields {
		if field.Name == name {
			return field.PositionalMany
		}
	}
	return false
}

func bindingRequestCoverage(request map[string]any) map[string]bool {
	covered := map[string]bool{}
	collectBindingRequestCoverage(covered, "", request)
	return covered
}

func collectBindingRequestCoverage(covered map[string]bool, prefix string, request map[string]any) {
	for key, value := range request {
		if key == "capture" || key == "raw" {
			continue
		}
		nextPrefix := key
		if prefix != "" {
			nextPrefix = prefix + "." + key
		}
		node, ok := value.(map[string]any)
		if !ok {
			covered[nextPrefix] = true
			continue
		}
		if _, ok := node["raw"]; ok {
			continue
		}
		if fields, ok := node["fields"].(map[string]any); ok {
			collectBindingRequestCoverage(covered, nextPrefix, fields)
			continue
		}
		if _, ok := node["from"]; ok {
			covered[nextPrefix] = true
			continue
		}
		if _, ok := node["each"]; ok {
			covered[nextPrefix] = true
			continue
		}
		collectBindingRequestCoverage(covered, nextPrefix, node)
	}
}

func hasLocalizedText(text LocalizedText) bool {
	return text.Text("en") != "" && text.Text("zh-CN") != ""
}

func requireLocalizedText(t *testing.T, path string, text LocalizedText) {
	t.Helper()
	if !hasLocalizedText(text) {
		t.Fatalf("%s must include en and zh-CN: %#v", path, text)
	}
}

func requireSchemaFieldLocalizedText(t *testing.T, path string, field SchemaField) {
	t.Helper()
	requireLocalizedText(t, path+".description", field.Description)
	if field.Items != nil {
		for name, child := range field.Items.Fields {
			requireSchemaFieldLocalizedText(t, path+".items.fields."+name, child)
		}
	}
	for name, child := range field.Fields {
		requireSchemaFieldLocalizedText(t, path+".fields."+name, child)
	}
}

func requireKeyExamples(t *testing.T, examples []string) {
	t.Helper()
	if len(examples) < 2 || len(examples) > 4 {
		t.Fatalf("examples must contain 2 to 4 entries: %#v", examples)
	}
	for _, example := range examples {
		if !strings.HasPrefix(example, "ecctl ") {
			t.Fatalf("example must start with ecctl: %q", example)
		}
	}
}

func requireProbeFieldPaths(t *testing.T, probe Probe, paths []string) {
	t.Helper()
	covered := map[string]string{}
	for field, mapping := range probe.Response.Fields {
		for _, path := range probeFieldPaths(mapping) {
			covered[path] = field
		}
	}
	missing := make([]string, 0)
	for _, path := range paths {
		if _, ok := covered[path]; !ok {
			missing = append(missing, path)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("probe %s missing response field paths: %s", probe.API, strings.Join(missing, ", "))
	}
}

func probeFieldPaths(field ProbeField) []string {
	paths := make([]string, 0)
	for _, path := range []string{field.Path, field.From, field.Lower, field.Int, field.Port} {
		if path != "" {
			paths = append(paths, path)
		}
	}
	paths = append(paths, field.First...)
	for _, child := range field.Each {
		paths = append(paths, probeFieldPaths(child)...)
	}
	return paths
}

func TestLoadResourceFromExplicitDir(t *testing.T) {
	loaded, err := LoadResource("../../specs", "vpc", "vpc")
	if err != nil {
		t.Fatalf("LoadResource: %v", err)
	}
	if loaded.Product != "vpc" || loaded.Resource != "vpc" {
		t.Fatalf("loaded wrong resource: %#v", loaded)
	}
}

func TestLoadResourceDefaultsToEmbeddedSpecOutsideRepo(t *testing.T) {
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(originalWD); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatalf("Chdir: %v", err)
	}

	loaded, err := LoadResource("", "vpc", "vpc")
	if err != nil {
		t.Fatalf("LoadResource: %v", err)
	}
	if loaded.Product != "vpc" || loaded.Resource != "vpc" {
		t.Fatalf("loaded wrong resource: %#v", loaded)
	}
}

func TestLoadResourceValidatesLoadedSpec(t *testing.T) {
	specDir := t.TempDir()
	resourceDir := filepath.Join(specDir, "vpc")
	if err := os.MkdirAll(resourceDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	raw := []byte(`
schema_version: 2
product: vpc
resource: vpc
kind: regional
schema:
  fields:
    id:
      type: string
waiters:
  broken:
    probe: missing
    target: available
`)
	if err := os.WriteFile(filepath.Join(resourceDir, "vpc.yaml"), raw, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := LoadResource(specDir, "vpc", "vpc")
	if err == nil || !strings.Contains(err.Error(), `references unknown probe "missing"`) {
		t.Fatalf("LoadResource error = %v, want validation error", err)
	}
}

func TestLoadFileReturnsReadError(t *testing.T) {
	if _, err := LoadFile(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Fatal("LoadFile succeeded for missing file")
	}
}

func TestLoadResourceRejectsMismatchedSpec(t *testing.T) {
	specDir := t.TempDir()
	writeSpecFile(t, specDir, "vpc", "vpc", `
schema_version: 2
product: ecs
resource: instance
kind: regional
schema:
  fields:
    id:
      type: string
`)

	_, err := LoadResource(specDir, "vpc", "vpc")
	if err == nil || !strings.Contains(err.Error(), "loaded ecs/instance from vpc/vpc") {
		t.Fatalf("LoadResource error = %v, want mismatch", err)
	}
}

func TestLoadDefaultResourceUsesGeneratedCatalog(t *testing.T) {
	ResetCacheForTest()
	loaded, err := LoadResource("", "vpc", "vpc")
	if err != nil {
		t.Fatalf("LoadResource: %v", err)
	}
	if loaded.Product != "vpc" || loaded.Resource != "vpc" {
		t.Fatalf("loaded wrong resource: %#v", loaded)
	}
}

func TestLoadDefaultResourceReturnsMissingError(t *testing.T) {
	ResetCacheForTest()
	if _, err := LoadResource("", "missing", "missing"); err == nil {
		t.Fatal("LoadResource succeeded for missing resource")
	}
}

func TestListResourcesFromExplicitDir(t *testing.T) {
	specDir := t.TempDir()
	writeSpecFile(t, specDir, "vpc", "vpc", `
schema_version: 2
product: vpc
resource: vpc
kind: regional
schema:
  fields:
    id:
      type: string
`)
	writeSpecFile(t, specDir, "ecs", "instance", `
schema_version: 2
product: ecs
resource: instance
kind: regional
schema:
  fields:
    id:
      type: string
`)
	writeSpecFile(t, specDir, "empty", "ignored", `
schema_version: 2
product: ""
resource: ""
kind: regional
`)
	if err := os.WriteFile(filepath.Join(specDir, "README.txt"), []byte("ignored"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	refs, err := ListResources(specDir)
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	want := []ResourceRef{{Product: "ecs", Resource: "instance"}, {Product: "vpc", Resource: "vpc"}}
	if !sameRefs(refs, want) {
		t.Fatalf("refs = %#v, want %#v", refs, want)
	}
}

func TestListResourcesDefaultsToRepositorySpecs(t *testing.T) {
	refs, err := ListResources("")
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if !containsRef(refs, ResourceRef{Product: "vpc", Resource: "vpc"}) {
		t.Fatalf("refs missing vpc/vpc: %#v", refs)
	}
}

func TestListResourcesFallsBackToEmbeddedSpecsOutsideRepo(t *testing.T) {
	withWorkingDir(t, t.TempDir())

	refs, err := ListResources("")
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if !containsRef(refs, ResourceRef{Product: "vpc", Resource: "vpc"}) {
		t.Fatalf("refs missing vpc/vpc: %#v", refs)
	}
}

func TestListResourcesReturnsLoadErrors(t *testing.T) {
	specDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(specDir, "vpc"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(specDir, "vpc", "broken.yaml"), []byte("unknown: true"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := ListResources(specDir); err == nil {
		t.Fatal("ListResources succeeded with invalid spec")
	}
}

func TestValidateReportsRequiredFields(t *testing.T) {
	tests := []struct {
		name string
		spec ResourceSpec
		want string
	}{
		{name: "schema version", spec: ResourceSpec{}, want: "schema_version is required"},
		{name: "product", spec: ResourceSpec{SchemaVersion: 2}, want: "product is required"},
		{name: "resource", spec: ResourceSpec{SchemaVersion: 2, Product: "ecs"}, want: "resource is required"},
		{name: "kind", spec: ResourceSpec{SchemaVersion: 2, Product: "ecs", Resource: "instance"}, want: "kind is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := Validate(tt.spec); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestValidateReportsReferenceErrors(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ResourceSpec)
		want   string
	}{
		{name: "probe api", mutate: func(spec *ResourceSpec) {
			spec.Probes["lookup"] = Probe{}
		}, want: `probe "lookup" api is required`},
		{name: "probe item and items", mutate: func(spec *ResourceSpec) {
			spec.Probes["lookup"] = Probe{API: "Describe", Response: ProbeResponse{Item: "$", Items: "$.Items"}}
		}, want: `probe "lookup" cannot set both response.items and response.item`},
		{name: "waiter probe", mutate: func(spec *ResourceSpec) {
			spec.Waiters["ready"] = Waiter{Target: "Available"}
		}, want: `waiter "ready" probe is required`},
		{name: "waiter unknown probe", mutate: func(spec *ResourceSpec) {
			spec.Waiters["ready"] = Waiter{Probe: "missing", Target: "Available"}
		}, want: `waiter "ready" references unknown probe "missing"`},
		{name: "waiter target", mutate: func(spec *ResourceSpec) {
			spec.Waiters["ready"] = Waiter{Probe: "lookup"}
		}, want: `waiter "ready" target is required`},
		{name: "binding api", mutate: func(spec *ResourceSpec) {
			spec.Bindings["create"] = Binding{Request: map[string]any{}}
		}, want: `binding "create" api is required`},
		{name: "binding idempotency prefix", mutate: func(spec *ResourceSpec) {
			spec.Bindings["create"] = Binding{API: "Create", Request: map[string]any{}, Idempotency: Idempotency{Prefix: "prefix"}}
		}, want: `binding "create" idempotency prefix requires field`},
		{name: "binding idempotency field", mutate: func(spec *ResourceSpec) {
			spec.Bindings["create"] = Binding{API: "Create", Request: map[string]any{}, Idempotency: Idempotency{Field: "ClientToken"}}
		}, want: `binding "create" idempotency field requires prefix`},
		{name: "binding retry policy", mutate: func(spec *ResourceSpec) {
			spec.Bindings["create"] = Binding{API: "Create", Request: map[string]any{}, Retry: TransitionRetry{Policy: "unknown"}}
		}, want: `binding "create": retry policy "unknown" is not supported`},
		{name: "binding unknown waiter", mutate: func(spec *ResourceSpec) {
			spec.Bindings["create"] = Binding{API: "Create", Request: map[string]any{}, Wait: "missing"}
		}, want: `binding "create" references unknown waiter "missing"`},
		{name: "operation unknown probe", mutate: func(spec *ResourceSpec) {
			spec.Operations["get"] = Operation{Call: OperationCall{Probe: "missing"}}
		}, want: `operation "get" references unknown probe "missing"`},
		{name: "operation require_when missing groups", mutate: func(spec *ResourceSpec) {
			spec.Operations["update"] = Operation{Input: OperationInput{Fields: OperationFields{{Name: "id"}}}, RequireWhen: []ConditionalRequirement{{WhenAny: []string{"id"}}}}
		}, want: `operation "update" require_when entry 0 requires when_any and require_any`},
		{name: "operation require_when unknown input", mutate: func(spec *ResourceSpec) {
			spec.Operations["update"] = Operation{Input: OperationInput{Fields: OperationFields{{Name: "id"}}}, RequireWhen: []ConditionalRequirement{{WhenAny: []string{"missing"}, RequireAny: []string{"id"}}}}
		}, want: `operation "update" require_when entry 0 references unknown input "missing"`},
		{name: "filter target", mutate: func(spec *ResourceSpec) {
			spec.Operations["list"] = Operation{Filters: map[string]Filter{"name": {}}}
		}, want: `operation "list" filter "name" target is required`},
		{name: "filter unknown input", mutate: func(spec *ResourceSpec) {
			spec.Operations["list"] = Operation{Filters: map[string]Filter{"name": {Target: "missing"}}}
		}, want: `operation "list" filter "name" references unknown input "missing"`},
		{name: "workflow unknown binding", mutate: func(spec *ResourceSpec) {
			spec.Operations["create"] = Operation{Workflow: []WorkflowStep{{Binding: "missing"}}}
		}, want: `operation "create" workflow step 0 references unknown binding "missing"`},
		{name: "workflow unknown probe", mutate: func(spec *ResourceSpec) {
			spec.Operations["get"] = Operation{Workflow: []WorkflowStep{{Probe: "missing"}}}
		}, want: `operation "get" workflow step 0 references unknown probe "missing"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := minimalValidSpec()
			tt.mutate(&spec)
			if err := Validate(spec); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate error = %v, want %q", err, tt.want)
			}
		})
	}
}

func minimalValidSpec() ResourceSpec {
	return ResourceSpec{
		SchemaVersion: 2,
		Product:       "ecs",
		Resource:      "instance",
		Kind:          "regional",
		Schema:        ResourceSchema{Fields: map[string]SchemaField{"name": {Type: "string"}}},
		Probes:        map[string]Probe{"lookup": {API: "DescribeInstances"}},
		Waiters:       map[string]Waiter{"ready": {Probe: "lookup", Target: "Running"}},
		Bindings:      map[string]Binding{"create": {API: "RunInstances", Request: map[string]any{}, Wait: "ready"}},
		Operations:    map[string]Operation{"get": {Call: OperationCall{Probe: "lookup"}}},
	}
}

func writeSpecFile(t *testing.T, root, product, resource, raw string) {
	t.Helper()
	dir := filepath.Join(root, product)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, resource+".yaml"), []byte(raw), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func withWorkingDir(t *testing.T, dir string) {
	t.Helper()
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(originalWD); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
}

func containsRef(refs []ResourceRef, want ResourceRef) bool {
	for _, ref := range refs {
		if ref == want {
			return true
		}
	}
	return false
}

func sameRefs(got, want []ResourceRef) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestLoadECSImageResourceLifecycleSpec(t *testing.T) {
	loaded, err := LoadFile("../../specs/ecs/image.yaml")
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if loaded.Product != "ecs" || loaded.Resource != "image" {
		t.Fatalf("loaded wrong resource: %s/%s", loaded.Product, loaded.Resource)
	}
	if loaded.Identity.Prefix != "m-" {
		t.Fatalf("identity prefix = %q, want m-", loaded.Identity.Prefix)
	}
	if loaded.Identity.OutputRoot.One != "image" || loaded.Identity.OutputRoot.Many != "images" {
		t.Fatalf("output roots = %#v, want image/images", loaded.Identity.OutputRoot)
	}
	for _, action := range []string{"list", "get", "create", "update", "delete", "copy", "export", "import"} {
		if _, ok := loaded.Operations[action]; !ok {
			t.Fatalf("image operation %q missing", action)
		}
	}
	for name, api := range map[string]string{
		"create_to_available":     "CreateImage",
		"update_attributes":       "ModifyImageAttribute",
		"update_share_permission": "ModifyImageSharePermission",
		"delete_to_absent":        "DeleteImage",
		"copy_to_destination":     "CopyImage",
		"cancel_copy":             "CancelCopyImage",
		"export_to_oss":           "ExportImage",
		"import_from_oss":         "ImportImage",
	} {
		if loaded.Bindings[name].API != api {
			t.Fatalf("binding %s API = %q, want %q", name, loaded.Bindings[name].API, api)
		}
	}
	for name, api := range map[string]string{
		"state":                    "DescribeImages",
		"share_permission":         "DescribeImageSharePermission",
		"from_family":              "DescribeImageFromFamily",
		"supported_instance_types": "DescribeImageSupportInstanceTypes",
		"task_attribute":           "DescribeTaskAttribute",
	} {
		if loaded.Probes[name].API != api {
			t.Fatalf("probe %s API = %q, want %q", name, loaded.Probes[name].API, api)
		}
	}
	createAvailable, ok := loaded.Waiters["available_after_create"]
	if !ok || createAvailable.Target != "Available" {
		t.Fatalf("available_after_create waiter = %#v", createAvailable)
	}
	if !hasString(createAvailable.Failure.States, "CreateFailed") {
		t.Fatalf("available_after_create.failure.states must list CreateFailed: %#v", createAvailable.Failure.States)
	}
	if loaded.Waiters["deleted_after_delete"].Target != "absent" {
		t.Fatalf("deleted_after_delete waiter = %#v", loaded.Waiters["deleted_after_delete"])
	}
	if loaded.Waiters["task_finished"].Probe != "task_attribute" || loaded.Waiters["task_finished"].Target != "Finished" {
		t.Fatalf("task_finished waiter = %#v", loaded.Waiters["task_finished"])
	}
	if loaded.Controls["force"].Default != false {
		t.Fatalf("image delete --force default = %#v, want false (safe default; force must be opt-in)", loaded.Controls["force"].Default)
	}
	if !loaded.Probes["state"].Response.Absent.WhenEmptyForRequestedIDs {
		t.Fatalf("state probe must mark when_empty_for_requested_ids so delete waiter recognizes absent")
	}
	for _, name := range []string{"id", "name", "status", "destination_region", "oss_bucket", "disk_device_mappings", "share_add", "share_remove"} {
		if _, ok := loaded.Schema.Fields[name]; !ok {
			t.Fatalf("image schema field %q missing", name)
		}
	}
	// copy conflict: --cancel cannot mix with destination-* flags.
	conflicts := loaded.Operations["copy"].Conflicts
	if len(conflicts) == 0 || !hasString(conflicts[0].Any, "cancel") || !hasString(conflicts[0].WithAny, "destination_region") {
		t.Fatalf("image copy conflicts entry must forbid cancel+destination_region: %#v", conflicts)
	}
	if err := Validate(loaded); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestLoadECSCommandResourceLifecycleSpec(t *testing.T) {
	loaded, err := LoadFile("../../specs/ecs/command.yaml")
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if loaded.Product != "ecs" || loaded.Resource != "command" {
		t.Fatalf("loaded wrong resource: %s/%s", loaded.Product, loaded.Resource)
	}
	if loaded.Identity.Prefix != "c-" {
		t.Fatalf("identity prefix = %q, want c-", loaded.Identity.Prefix)
	}
	for _, action := range []string{"list", "get", "create", "update", "delete", "invoke", "stop"} {
		if _, ok := loaded.Operations[action]; !ok {
			t.Fatalf("command operation %q missing", action)
		}
	}
	for name, api := range map[string]string{
		"create_to_available":         "CreateCommand",
		"update_attributes":           "ModifyCommand",
		"update_invocation_attribute": "ModifyInvocationAttribute",
		"delete_to_absent":            "DeleteCommand",
		"invoke_command":              "InvokeCommand",
		"stop_invocation":             "StopInvocation",
	} {
		if loaded.Bindings[name].API != api {
			t.Fatalf("binding %s API = %q, want %q", name, loaded.Bindings[name].API, api)
		}
	}
	for name, api := range map[string]string{
		"state":             "DescribeCommands",
		"invocation":        "DescribeInvocations",
		"invocation_result": "DescribeInvocationResults",
	} {
		if loaded.Probes[name].API != api {
			t.Fatalf("probe %s API = %q, want %q", name, loaded.Probes[name].API, api)
		}
	}
	finished, ok := loaded.Waiters["finished_after_invoke"]
	if !ok || finished.Target != "Finished" {
		t.Fatalf("finished_after_invoke waiter = %#v", finished)
	}
	for _, state := range []string{"Failed", "PartialFailed", "Stopped"} {
		if !hasString(finished.Failure.States, state) {
			t.Fatalf("finished_after_invoke.failure.states must list %q: %#v", state, finished.Failure.States)
		}
	}
	if loaded.Waiters["stopped_after_stop"].Target != "Stopped" {
		t.Fatalf("stopped_after_stop waiter = %#v", loaded.Waiters["stopped_after_stop"])
	}
	if loaded.Waiters["deleted_after_delete"].Target != "absent" {
		t.Fatalf("deleted_after_delete waiter = %#v", loaded.Waiters["deleted_after_delete"])
	}
	if loaded.Controls["force"].Default != false {
		t.Fatalf("command stop --force default = %#v, want false (safe default; force must be opt-in)", loaded.Controls["force"].Default)
	}
	if !loaded.Probes["state"].Response.Absent.WhenEmptyForRequestedIDs {
		t.Fatalf("state probe must mark when_empty_for_requested_ids so delete waiter recognizes absent")
	}
	// Idempotent invocation create.
	if loaded.Bindings["create_to_available"].Idempotency.Field != "ClientToken" {
		t.Fatalf("CreateCommand must use ClientToken idempotency: %#v", loaded.Bindings["create_to_available"].Idempotency)
	}
	if loaded.Bindings["invoke_command"].Idempotency.Field != "ClientToken" {
		t.Fatalf("InvokeCommand must use ClientToken idempotency: %#v", loaded.Bindings["invoke_command"].Idempotency)
	}
	// update conflict: --invocation-id cannot mix with command-template fields.
	conflicts := loaded.Operations["update"].Conflicts
	if len(conflicts) == 0 || !hasString(conflicts[0].WithAny, "invocation_id") {
		t.Fatalf("command update conflicts must protect invocation_id from template fields: %#v", conflicts)
	}
	for _, name := range []string{"id", "invocation_id", "command_content", "instance_ids", "parameters", "frequency", "repeat_mode"} {
		if _, ok := loaded.Schema.Fields[name]; !ok {
			t.Fatalf("command schema field %q missing", name)
		}
	}
	if err := Validate(loaded); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestLoadECSRegionResourceLifecycleSpec(t *testing.T) {
	loaded, err := LoadFile("../../specs/ecs/region.yaml")
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if loaded.Product != "ecs" || loaded.Resource != "region" {
		t.Fatalf("loaded wrong resource: %s/%s", loaded.Product, loaded.Resource)
	}
	if loaded.Identity.OutputRoot.One != "region" || loaded.Identity.OutputRoot.Many != "regions" {
		t.Fatalf("output roots = %#v, want region/regions", loaded.Identity.OutputRoot)
	}
	if _, ok := loaded.Operations["list"]; !ok {
		t.Fatalf("region must expose list")
	}
	// Read-only catalog: list only, no mutating verbs.
	for _, forbidden := range []string{"create", "update", "delete", "get"} {
		if _, ok := loaded.Operations[forbidden]; ok {
			t.Fatalf("region must not expose %q (read-only catalog)", forbidden)
		}
	}
	if loaded.Probes["list"].API != "DescribeRegions" {
		t.Fatalf("list probe API = %q, want DescribeRegions", loaded.Probes["list"].API)
	}
	if got := loaded.Probes["list"].Response.Items; got != "$.Regions.Region" {
		t.Fatalf("list probe items = %q, want $.Regions.Region", got)
	}
	if len(loaded.Bindings) != 0 {
		t.Fatalf("region is read-only and must declare no bindings: %#v", loaded.Bindings)
	}
	if err := Validate(loaded); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestLoadECSZoneResourceLifecycleSpec(t *testing.T) {
	loaded, err := LoadFile("../../specs/ecs/zone.yaml")
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if loaded.Product != "ecs" || loaded.Resource != "zone" {
		t.Fatalf("loaded wrong resource: %s/%s", loaded.Product, loaded.Resource)
	}
	if loaded.Identity.OutputRoot.One != "zone" || loaded.Identity.OutputRoot.Many != "zones" {
		t.Fatalf("output roots = %#v, want zone/zones", loaded.Identity.OutputRoot)
	}
	if _, ok := loaded.Operations["list"]; !ok {
		t.Fatalf("zone must expose list")
	}
	for _, forbidden := range []string{"create", "update", "delete", "get"} {
		if _, ok := loaded.Operations[forbidden]; ok {
			t.Fatalf("zone must not expose %q (read-only catalog)", forbidden)
		}
	}
	if loaded.Probes["list"].API != "DescribeZones" {
		t.Fatalf("list probe API = %q, want DescribeZones", loaded.Probes["list"].API)
	}
	if got := loaded.Probes["list"].Response.Items; got != "$.Zones.Zone" {
		t.Fatalf("list probe items = %q, want $.Zones.Zone", got)
	}
	// DescribeZones requires RegionId — confirm the probe binds it from context.
	if got := loaded.Probes["list"].Request["RegionId"]; got != "$context.region" {
		t.Fatalf("zone list probe RegionId = %#v, want $context.region", got)
	}
	if len(loaded.Bindings) != 0 {
		t.Fatalf("zone is read-only and must declare no bindings: %#v", loaded.Bindings)
	}
	if err := Validate(loaded); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestLoadECSKeypairResourceLifecycleSpec(t *testing.T) {
	loaded, err := LoadFile("../../specs/ecs/keypair.yaml")
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if loaded.Product != "ecs" || loaded.Resource != "keypair" {
		t.Fatalf("loaded wrong resource: %s/%s", loaded.Product, loaded.Resource)
	}
	for _, action := range []string{"list", "get", "create", "delete"} {
		if _, ok := loaded.Operations[action]; !ok {
			t.Fatalf("keypair operation %q missing", action)
		}
	}
	for name, api := range map[string]string{
		"create_to_available": "CreateKeyPair",
		"import_to_available": "ImportKeyPair",
		"delete_keypairs":     "DeleteKeyPairs",
	} {
		if loaded.Bindings[name].API != api {
			t.Fatalf("binding %s API = %q, want %q", name, loaded.Bindings[name].API, api)
		}
	}
	if loaded.Probes["list"].API != "DescribeKeyPairs" {
		t.Fatalf("list probe API = %q, want DescribeKeyPairs", loaded.Probes["list"].API)
	}
	if err := Validate(loaded); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestLoadECSAssistantResourceLifecycleSpec(t *testing.T) {
	loaded, err := LoadFile("../../specs/ecs/assistant.yaml")
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if loaded.Product != "ecs" || loaded.Resource != "assistant" {
		t.Fatalf("loaded wrong resource: %s/%s", loaded.Product, loaded.Resource)
	}
	for _, action := range []string{"get", "update", "install"} {
		if _, ok := loaded.Operations[action]; !ok {
			t.Fatalf("assistant operation %q missing", action)
		}
	}
	// Command execution stays on instance; assistant must not expose exec/sendfile.
	for _, forbidden := range []string{"exec", "sendfile", "create", "delete", "list"} {
		if _, ok := loaded.Operations[forbidden]; ok {
			t.Fatalf("assistant must not expose %q", forbidden)
		}
	}
	if loaded.Bindings["update_settings"].API != "ModifyCloudAssistantSettings" {
		t.Fatalf("update binding = %#v", loaded.Bindings["update_settings"])
	}
	if got := loaded.Bindings["update_settings"].Request["SettingType"]; got != "$.setting_type" {
		t.Fatalf("update SettingType = %#v, want $.setting_type", got)
	}
	if loaded.Probes["updated_setting"].API != "DescribeCloudAssistantSettings" {
		t.Fatalf("updated_setting probe API = %q", loaded.Probes["updated_setting"].API)
	}
	if loaded.Bindings["install_agent"].API != "InstallCloudAssistant" {
		t.Fatalf("install binding = %#v", loaded.Bindings["install_agent"])
	}
	if loaded.Probes["settings"].API != "DescribeCloudAssistantSettings" {
		t.Fatalf("settings probe API = %q", loaded.Probes["settings"].API)
	}
	if loaded.Probes["status"].API != "DescribeCloudAssistantStatus" {
		t.Fatalf("status probe API = %q", loaded.Probes["status"].API)
	}
	if err := Validate(loaded); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestLoadECSAutoSnapshotPolicyResourceLifecycleSpec(t *testing.T) {
	loaded, err := LoadFile("../../specs/ecs/auto-snapshot-policy.yaml")
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if loaded.Product != "ecs" || loaded.Resource != "auto-snapshot-policy" {
		t.Fatalf("loaded wrong resource: %s/%s", loaded.Product, loaded.Resource)
	}
	if !hasString(loaded.Aliases, "sp") {
		t.Fatalf("auto-snapshot-policy must alias sp: %#v", loaded.Aliases)
	}
	for _, action := range []string{"list", "get", "create", "update", "delete"} {
		if _, ok := loaded.Operations[action]; !ok {
			t.Fatalf("auto-snapshot-policy operation %q missing", action)
		}
	}
	for name, api := range map[string]string{
		"create_to_available": "CreateAutoSnapshotPolicy",
		"update_attributes":   "ModifyAutoSnapshotPolicyEx",
		"apply_to_disks":      "ApplyAutoSnapshotPolicy",
		"cancel_from_disks":   "CancelAutoSnapshotPolicy",
		"delete_to_absent":    "DeleteAutoSnapshotPolicy",
	} {
		if loaded.Bindings[name].API != api {
			t.Fatalf("binding %s API = %q, want %q", name, loaded.Bindings[name].API, api)
		}
	}
	// The Ex policy APIs use lowercase-first parameter names.
	if got := loaded.Bindings["create_to_available"].Request["timePoints"]; got != "$.time_points" {
		t.Fatalf("CreateAutoSnapshotPolicy must use lowercase timePoints param: %#v", got)
	}
	if got := loaded.Bindings["create_to_available"].Request["regionId"]; got != "$context.region" {
		t.Fatalf("CreateAutoSnapshotPolicy must use lowercase regionId param: %#v", got)
	}
	if loaded.Probes["list"].API != "DescribeAutoSnapshotPolicyEx" {
		t.Fatalf("list probe API = %q", loaded.Probes["list"].API)
	}
	if err := Validate(loaded); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestLoadECSSnapshotGroupResourceLifecycleSpec(t *testing.T) {
	loaded, err := LoadFile("../../specs/ecs/snapshot-group.yaml")
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if loaded.Product != "ecs" || loaded.Resource != "snapshot-group" {
		t.Fatalf("loaded wrong resource: %s/%s", loaded.Product, loaded.Resource)
	}
	if !hasString(loaded.Aliases, "ssg") {
		t.Fatalf("snapshot-group must alias ssg: %#v", loaded.Aliases)
	}
	for name, api := range map[string]string{
		"create_to_accomplished": "CreateSnapshotGroup",
		"update_attributes":      "ModifySnapshotGroup",
		"delete_to_absent":       "DeleteSnapshotGroup",
	} {
		if loaded.Bindings[name].API != api {
			t.Fatalf("binding %s API = %q, want %q", name, loaded.Bindings[name].API, api)
		}
	}
	if loaded.Probes["list"].API != "DescribeSnapshotGroups" {
		t.Fatalf("list probe API = %q", loaded.Probes["list"].API)
	}
	// Token pagination — next_token control, no page control.
	listOp := loaded.Operations["list"]
	if !operationFieldPresent(listOp.Input.Controls, "next_token") || operationFieldPresent(listOp.Input.Controls, "page") {
		t.Fatalf("snapshot-group list must use next_token pagination, not page: %#v", listOp.Input.Controls)
	}
	if loaded.Waiters["accomplished_after_create"].Target != "accomplished" {
		t.Fatalf("create waiter target = %q, want accomplished", loaded.Waiters["accomplished_after_create"].Target)
	}
	if err := Validate(loaded); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestLoadECSSnapshotResourceLifecycleSpec(t *testing.T) {
	loaded, err := LoadFile("../../specs/ecs/snapshot.yaml")
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if loaded.Product != "ecs" || loaded.Resource != "snapshot" {
		t.Fatalf("loaded wrong resource: %s/%s", loaded.Product, loaded.Resource)
	}
	for _, action := range []string{"list", "get", "create", "update", "delete", "copy"} {
		if _, ok := loaded.Operations[action]; !ok {
			t.Fatalf("snapshot operation %q missing", action)
		}
	}
	for name, api := range map[string]string{
		"create_to_accomplished": "CreateSnapshot",
		"update_attributes":      "ModifySnapshotAttribute",
		"modify_category":        "ModifySnapshotCategory",
		"lock_snapshot":          "LockSnapshot",
		"unlock_snapshot":        "UnlockSnapshot",
		"open_snapshot_service":  "OpenSnapshotService",
		"delete_to_absent":       "DeleteSnapshot",
		"copy_to_destination":    "CopySnapshot",
	} {
		if loaded.Bindings[name].API != api {
			t.Fatalf("binding %s API = %q, want %q", name, loaded.Bindings[name].API, api)
		}
	}
	for name, api := range map[string]string{
		"list":    "DescribeSnapshots",
		"locked":  "DescribeLockedSnapshots",
		"links":   "DescribeSnapshotLinks",
		"monitor": "DescribeSnapshotMonitorData",
		"package": "DescribeSnapshotPackage",
		"usage":   "DescribeSnapshotsUsage",
	} {
		if loaded.Probes[name].API != api {
			t.Fatalf("probe %s API = %q, want %q", name, loaded.Probes[name].API, api)
		}
	}
	if loaded.Controls["force"].Default != false {
		t.Fatalf("snapshot delete --force default = %#v, want false", loaded.Controls["force"].Default)
	}
	if loaded.Waiters["accomplished_after_create"].Target != "accomplished" {
		t.Fatalf("create waiter target = %q, want accomplished", loaded.Waiters["accomplished_after_create"].Target)
	}
	// lock/unlock must be mutually exclusive.
	conflicts := loaded.Operations["update"].Conflicts
	if len(conflicts) == 0 || !hasString(conflicts[0].Any, "lock") || !hasString(conflicts[0].WithAny, "unlock") {
		t.Fatalf("snapshot update must forbid lock+unlock together: %#v", conflicts)
	}
	if err := Validate(loaded); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

// TestBuiltInSpecsDoNotShadowGlobalFlags guards against the class of bug where
// a resource declares an input field whose CLI flag collides with a global
// persistent flag (e.g. a field literally named "region"). Such a field
// shadows the global flag, so the global value is never populated — which is
// how `rg resource list --region X` once silently lost its region.
func TestBuiltInSpecsDoNotShadowGlobalFlags(t *testing.T) {
	reserved := map[string]bool{
		"region": true, "profile": true, "lang": true,
		"output": true, "json": true, "no-color": true, "help": true,
	}
	allowed := map[string]bool{
		// ACK CreateCluster uses body.profile for the cluster scenario profile.
		// The CLI has a regression test ensuring this command-local --profile
		// does not select the ecctl configuration profile.
		"ack/ack/create/profile": true,
	}
	root := "../../specs"
	var files []string
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".yaml" || filepath.Base(path) == "product.yaml" {
			return nil
		}
		files = append(files, path)
		return nil
	}); err != nil {
		t.Fatalf("walk specs: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no resource specs found")
	}
	for _, path := range files {
		loaded, err := LoadFile(path)
		if err != nil {
			t.Fatalf("LoadFile(%s): %v", path, err)
		}
		if loaded.Resource == "" {
			continue
		}
		for opName, operation := range loaded.Operations {
			fields := append(append(OperationFields{}, operation.Input.Fields...), operation.Input.Controls...)
			for _, field := range fields {
				flag := operationFieldCLIFlagName(field)
				key := loaded.Product + "/" + loaded.Resource + "/" + opName + "/" + field.Name
				if allowed[key] {
					continue
				}
				if reserved[flag] {
					t.Errorf("%s/%s operation %q input %q produces flag --%s which shadows a global persistent flag; rename the field or set a non-colliding flag_name",
						loaded.Product, loaded.Resource, opName, field.Name, flag)
				}
			}
		}
	}
}
