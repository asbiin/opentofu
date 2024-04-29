// Copyright (c) The OpenTofu Authors
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2023 HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package configs

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"

	"github.com/opentofu/opentofu/internal/addrs"
	"github.com/opentofu/opentofu/internal/getmodules"
	"github.com/opentofu/opentofu/internal/tfdiags"
)

// TestCommand represents the OpenTofu a given run block will execute, plan
// or apply. Defaults to apply.
type TestCommand rune

// TestMode represents the plan mode that OpenTofu will use for a given run
// block, normal or refresh-only. Defaults to normal.
type TestMode rune

const (
	// ApplyTestCommand causes the run block to execute a OpenTofu apply
	// operation.
	ApplyTestCommand TestCommand = 0

	// PlanTestCommand causes the run block to execute a OpenTofu plan
	// operation.
	PlanTestCommand TestCommand = 'P'

	// NormalTestMode causes the run block to execute in plans.NormalMode.
	NormalTestMode TestMode = 0

	// RefreshOnlyTestMode causes the run block to execute in
	// plans.RefreshOnlyMode.
	RefreshOnlyTestMode TestMode = 'R'
)

// TestFile represents a single test file within a `tofu test` execution.
//
// A test file is made up of a sequential list of run blocks, each designating
// a command to execute and a series of validations to check after the command.
type TestFile struct {
	// Variables defines a set of global variable definitions that should be set
	// for every run block within the test file.
	Variables map[string]hcl.Expression

	// Providers defines a set of providers that are available to run blocks
	// within this test file.
	//
	// If empty, tests should use the default providers for the module under
	// test.
	Providers map[string]*Provider

	// Runs defines the sequential list of run blocks that should be executed in
	// order.
	Runs []*TestRun

	VariablesDeclRange hcl.Range
}

// TestRun represents a single run block within a test file.
//
// Each run block represents a single OpenTofu command to be executed and a set
// of validations to run after the command.
type TestRun struct {
	Name string

	// Command is the OpenTofu command to execute.
	//
	// One of ['apply', 'plan'].
	Command TestCommand

	// Options contains the embedded plan options that will affect the given
	// Command. These should map to the options documented here:
	//   - https://opentofu.org/docs/cli/commands/plan/#planning-options
	//
	// Note, that the Variables are a top level concept and not embedded within
	// the options despite being listed as plan options in the documentation.
	Options *TestRunOptions

	// Variables defines a set of variable definitions for this command.
	//
	// Any variables specified locally that clash with the global variables will
	// take precedence over the global definition.
	Variables map[string]hcl.Expression

	// Providers specifies the set of providers that should be loaded into the
	// module for this run block.
	//
	// Providers specified here must be configured in one of the provider blocks
	// for this file. If empty, the run block will load the default providers
	// for the module under test.
	Providers []PassedProviderConfig

	// CheckRules defines the list of assertions/validations that should be
	// checked by this run block.
	CheckRules []*CheckRule

	// Module defines an address of another module that should be loaded and
	// executed as part of this run block instead of the module under test.
	//
	// In the initial version of the testing framework we will only support
	// loading alternate modules from local directories or the registry.
	Module *TestRunModuleCall

	// ConfigUnderTest describes the configuration this run block should execute
	// against.
	//
	// In typical cases, this will be null and the config under test is the
	// configuration within the directory the tofu test command is
	// executing within. However, when Module is set the config under test is
	// whichever config is defined by Module. This field is then set during the
	// configuration load process and should be used when the test is executed.
	ConfigUnderTest *Config

	// ExpectFailures should be a list of checkable objects that are expected
	// to report a failure from their custom conditions as part of this test
	// run.
	ExpectFailures []hcl.Traversal

	// OverrideResources is a list of resources to be overriden with static values.
	// Underlying providers shouldn't be called for overriden resources.
	OverrideResources []*OverrideResource

	// OverrideModules is a list of modules to be overriden with static values.
	// Underlying modules shouldn't be called.
	OverrideModules []*OverrideModule

	NameDeclRange      hcl.Range
	VariablesDeclRange hcl.Range
	DeclRange          hcl.Range
}

// Validate does a very simple and cursory check across the run block to look
// for simple issues we can highlight early on.
func (run *TestRun) Validate() tfdiags.Diagnostics {
	var diags tfdiags.Diagnostics

	// We want to make sure all the ExpectFailure references
	// are the correct kind of reference.
	for _, traversal := range run.ExpectFailures {

		reference, refDiags := addrs.ParseRefFromTestingScope(traversal)
		diags = diags.Append(refDiags)
		if refDiags.HasErrors() {
			continue
		}

		switch reference.Subject.(type) {
		// You can only reference outputs, inputs, checks, and resources.
		case addrs.OutputValue, addrs.InputVariable, addrs.Check, addrs.ResourceInstance, addrs.Resource:
			// Do nothing, these are okay!
		default:
			diags = diags.Append(&hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "Invalid `expect_failures` reference",
				Detail:   fmt.Sprintf("You cannot expect failures from %s. You can only expect failures from checkable objects such as input variables, output values, check blocks, managed resources and data sources.", reference.Subject.String()),
				Subject:  reference.SourceRange.ToHCL().Ptr(),
			})
		}

	}

	return diags
}

// TestRunModuleCall specifies which module should be executed by a given run
// block.
type TestRunModuleCall struct {
	// Source is the source of the module to test.
	Source addrs.ModuleSource

	// Version is the version of the module to load from the registry.
	Version VersionConstraint

	DeclRange       hcl.Range
	SourceDeclRange hcl.Range
}

// TestRunOptions contains the plan options for a given run block.
type TestRunOptions struct {
	// Mode is the planning mode to run in. One of ['normal', 'refresh-only'].
	Mode TestMode

	// Refresh is analogous to the -refresh=false OpenTofu plan option.
	Refresh bool

	// Replace is analogous to the -refresh=ADDRESS OpenTofu plan option.
	Replace []hcl.Traversal

	// Target is analogous to the -target=ADDRESS OpenTofu plan option.
	Target []hcl.Traversal

	DeclRange hcl.Range
}

// OverrideResource contains information about a resource or data block to be overriden.
type OverrideResource struct {
	// Target references resource or data block to override.
	Target       hcl.Traversal
	TargetParsed *addrs.ConfigResource

	// Mode indicates if the Target is resource or data block.
	Mode addrs.ResourceMode

	// Values represents fields to use as defaults
	// if they are not present in configuration.
	Values map[string]cty.Value
}

// OverrideModule contains information about a module to be overriden.
type OverrideModule struct {
	Target       hcl.Traversal
	TargetParsed addrs.Module
	Outputs      map[string]cty.Value
}

func loadTestFile(body hcl.Body) (*TestFile, hcl.Diagnostics) {
	var diags hcl.Diagnostics

	content, contentDiags := body.Content(testFileSchema)
	diags = append(diags, contentDiags...)

	tf := TestFile{
		Providers: make(map[string]*Provider),
	}

	for _, block := range content.Blocks {
		switch block.Type {
		case "run":
			run, runDiags := decodeTestRunBlock(block)
			diags = append(diags, runDiags...)
			if !runDiags.HasErrors() {
				tf.Runs = append(tf.Runs, run)
			}
		case "variables":
			if tf.Variables != nil {
				diags = append(diags, &hcl.Diagnostic{
					Severity: hcl.DiagError,
					Summary:  "Multiple \"variables\" blocks",
					Detail:   fmt.Sprintf("This test file already has a variables block defined at %s.", tf.VariablesDeclRange),
					Subject:  block.DefRange.Ptr(),
				})
				continue
			}

			tf.Variables = make(map[string]hcl.Expression)
			tf.VariablesDeclRange = block.DefRange

			vars, varsDiags := block.Body.JustAttributes()
			diags = append(diags, varsDiags...)
			for _, v := range vars {
				tf.Variables[v.Name] = v.Expr
			}
		case "provider":
			provider, providerDiags := decodeProviderBlock(block)
			diags = append(diags, providerDiags...)
			if provider != nil {
				tf.Providers[provider.moduleUniqueKey()] = provider
			}
		}
	}

	return &tf, diags
}

func decodeTestRunBlock(block *hcl.Block) (*TestRun, hcl.Diagnostics) {
	var diags hcl.Diagnostics

	content, contentDiags := block.Body.Content(testRunBlockSchema)
	diags = append(diags, contentDiags...)

	r := TestRun{
		Name:          block.Labels[0],
		NameDeclRange: block.LabelRanges[0],
		DeclRange:     block.DefRange,
	}

	if !hclsyntax.ValidIdentifier(r.Name) {
		diags = append(diags, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Invalid run block name",
			Detail:   badIdentifierDetail,
			Subject:  &block.LabelRanges[0],
		})
	}

	for _, block := range content.Blocks {
		switch block.Type {
		case "assert":
			cr, crDiags := decodeCheckRuleBlock(block, false)
			diags = append(diags, crDiags...)
			if !crDiags.HasErrors() {
				r.CheckRules = append(r.CheckRules, cr)
			}
		case "plan_options":
			if r.Options != nil {
				diags = append(diags, &hcl.Diagnostic{
					Severity: hcl.DiagError,
					Summary:  "Multiple \"plan_options\" blocks",
					Detail:   fmt.Sprintf("This run block already has a plan_options block defined at %s.", r.Options.DeclRange),
					Subject:  block.DefRange.Ptr(),
				})
				continue
			}

			opts, optsDiags := decodeTestRunOptionsBlock(block)
			diags = append(diags, optsDiags...)
			if !optsDiags.HasErrors() {
				r.Options = opts
			}
		case "variables":
			if r.Variables != nil {
				diags = append(diags, &hcl.Diagnostic{
					Severity: hcl.DiagError,
					Summary:  "Multiple \"variables\" blocks",
					Detail:   fmt.Sprintf("This run block already has a variables block defined at %s.", r.VariablesDeclRange),
					Subject:  block.DefRange.Ptr(),
				})
				continue
			}

			r.Variables = make(map[string]hcl.Expression)
			r.VariablesDeclRange = block.DefRange

			vars, varsDiags := block.Body.JustAttributes()
			diags = append(diags, varsDiags...)
			for _, v := range vars {
				r.Variables[v.Name] = v.Expr
			}
		case "module":
			if r.Module != nil {
				diags = append(diags, &hcl.Diagnostic{
					Severity: hcl.DiagError,
					Summary:  "Multiple \"module\" blocks",
					Detail:   fmt.Sprintf("This run block already has a module block defined at %s.", r.Module.DeclRange),
					Subject:  block.DefRange.Ptr(),
				})
			}

			module, moduleDiags := decodeTestRunModuleBlock(block)
			diags = append(diags, moduleDiags...)
			if !moduleDiags.HasErrors() {
				r.Module = module
			}

		case "override_resource":
			overrideRes, overrideResDiags := decodeOverrideResourceBlock(block, addrs.ManagedResourceMode)
			diags = append(diags, overrideResDiags...)
			if !overrideResDiags.HasErrors() {
				r.OverrideResources = append(r.OverrideResources, overrideRes)
			}

		case "override_data":
			overrideData, overrideDataDiags := decodeOverrideResourceBlock(block, addrs.DataResourceMode)
			diags = append(diags, overrideDataDiags...)
			if !overrideDataDiags.HasErrors() {
				r.OverrideResources = append(r.OverrideResources, overrideData)
			}

		case "override_module":
			overrideMod, overrideModDiags := decodeOverrideModuleBlock(block)
			diags = append(diags, overrideModDiags...)
			if !overrideModDiags.HasErrors() {
				r.OverrideModules = append(r.OverrideModules, overrideMod)
			}
		}
	}

	if r.Variables == nil {
		// There is no distinction between a nil map of variables or an empty
		// map, but we can avoid any potential nil pointer exceptions by just
		// creating an empty map.
		r.Variables = make(map[string]hcl.Expression)
	}

	if r.Options == nil {
		// Create an options with default values if the user didn't specify
		// anything.
		r.Options = &TestRunOptions{
			Mode:    NormalTestMode,
			Refresh: true,
		}
	}

	if attr, exists := content.Attributes["command"]; exists {
		switch hcl.ExprAsKeyword(attr.Expr) {
		case "apply":
			r.Command = ApplyTestCommand
		case "plan":
			r.Command = PlanTestCommand
		default:
			diags = append(diags, &hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "Invalid \"command\" keyword",
				Detail:   "The \"command\" argument requires one of the following keywords without quotes: apply or plan.",
				Subject:  attr.Expr.Range().Ptr(),
			})
		}
	} else {
		r.Command = ApplyTestCommand // Default to apply
	}

	if attr, exists := content.Attributes["providers"]; exists {
		providers, providerDiags := decodePassedProviderConfigs(attr)
		diags = append(diags, providerDiags...)
		r.Providers = append(r.Providers, providers...)
	}

	if attr, exists := content.Attributes["expect_failures"]; exists {
		failures, failDiags := decodeDependsOn(attr)
		diags = append(diags, failDiags...)
		r.ExpectFailures = failures
	}

	return &r, diags
}

func decodeTestRunModuleBlock(block *hcl.Block) (*TestRunModuleCall, hcl.Diagnostics) {
	var diags hcl.Diagnostics

	content, contentDiags := block.Body.Content(testRunModuleBlockSchema)
	diags = append(diags, contentDiags...)

	module := TestRunModuleCall{
		DeclRange: block.DefRange,
	}

	haveVersionArg := false
	if attr, exists := content.Attributes["version"]; exists {
		var versionDiags hcl.Diagnostics
		module.Version, versionDiags = decodeVersionConstraint(attr)
		diags = append(diags, versionDiags...)
		haveVersionArg = true
	}

	if attr, exists := content.Attributes["source"]; exists {
		module.SourceDeclRange = attr.Range

		var raw string
		rawDiags := gohcl.DecodeExpression(attr.Expr, nil, &raw)
		diags = append(diags, rawDiags...)
		if !rawDiags.HasErrors() {
			var err error
			if haveVersionArg {
				module.Source, err = addrs.ParseModuleSourceRegistry(raw)
			} else {
				module.Source, err = addrs.ParseModuleSource(raw)
			}
			if err != nil {
				// NOTE: We leave mc.SourceAddr as nil for any situation where the
				// source attribute is invalid, so any code which tries to carefully
				// use the partial result of a failed config decode must be
				// resilient to that.
				module.Source = nil

				// NOTE: In practice it's actually very unlikely to end up here,
				// because our source address parser can turn just about any string
				// into some sort of remote package address, and so for most errors
				// we'll detect them only during module installation. There are
				// still a _few_ purely-syntax errors we can catch at parsing time,
				// though, mostly related to remote package sub-paths and local
				// paths.
				switch err := err.(type) {
				case *getmodules.MaybeRelativePathErr:
					diags = append(diags, &hcl.Diagnostic{
						Severity: hcl.DiagError,
						Summary:  "Invalid module source address",
						Detail: fmt.Sprintf(
							"OpenTofu failed to determine your intended installation method for remote module package %q.\n\nIf you intended this as a path relative to the current module, use \"./%s\" instead. The \"./\" prefix indicates that the address is a relative filesystem path.",
							err.Addr, err.Addr,
						),
						Subject: module.SourceDeclRange.Ptr(),
					})
				default:
					if haveVersionArg {
						// In this case we'll include some extra context that
						// we assumed a registry source address due to the
						// version argument.
						diags = append(diags, &hcl.Diagnostic{
							Severity: hcl.DiagError,
							Summary:  "Invalid registry module source address",
							Detail:   fmt.Sprintf("Failed to parse module registry address: %s.\n\nOpenTofu assumed that you intended a module registry source address because you also set the argument \"version\", which applies only to registry modules.", err),
							Subject:  module.SourceDeclRange.Ptr(),
						})
					} else {
						diags = append(diags, &hcl.Diagnostic{
							Severity: hcl.DiagError,
							Summary:  "Invalid module source address",
							Detail:   fmt.Sprintf("Failed to parse module source address: %s.", err),
							Subject:  module.SourceDeclRange.Ptr(),
						})
					}
				}
			}

			switch module.Source.(type) {
			case addrs.ModuleSourceRemote:
				// We only support local or registry modules when loading
				// modules directly from alternate sources during a test
				// execution.
				diags = append(diags, &hcl.Diagnostic{
					Severity: hcl.DiagError,
					Summary:  "Invalid module source address",
					Detail:   "Only local or registry module sources are currently supported from within test run blocks.",
					Subject:  module.SourceDeclRange.Ptr(),
				})
			}
		}
	} else {
		// Must have a source attribute.
		diags = append(diags, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Missing \"source\" attribute for module block",
			Detail:   "You must specify a source attribute when executing alternate modules during test executions.",
			Subject:  module.DeclRange.Ptr(),
		})
	}

	return &module, diags
}

func decodeTestRunOptionsBlock(block *hcl.Block) (*TestRunOptions, hcl.Diagnostics) {
	var diags hcl.Diagnostics

	content, contentDiags := block.Body.Content(testRunOptionsBlockSchema)
	diags = append(diags, contentDiags...)

	opts := TestRunOptions{
		DeclRange: block.DefRange,
	}

	if attr, exists := content.Attributes["mode"]; exists {
		switch hcl.ExprAsKeyword(attr.Expr) {
		case "refresh-only":
			opts.Mode = RefreshOnlyTestMode
		case "normal":
			opts.Mode = NormalTestMode
		default:
			diags = append(diags, &hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "Invalid \"mode\" keyword",
				Detail:   "The \"mode\" argument requires one of the following keywords without quotes: normal or refresh-only",
				Subject:  attr.Expr.Range().Ptr(),
			})
		}
	} else {
		opts.Mode = NormalTestMode // Default to normal
	}

	if attr, exists := content.Attributes["refresh"]; exists {
		diags = append(diags, gohcl.DecodeExpression(attr.Expr, nil, &opts.Refresh)...)
	} else {
		// Defaults to true.
		opts.Refresh = true
	}

	if attr, exists := content.Attributes["replace"]; exists {
		reps, repsDiags := decodeDependsOn(attr)
		diags = append(diags, repsDiags...)
		opts.Replace = reps
	}

	if attr, exists := content.Attributes["target"]; exists {
		tars, tarsDiags := decodeDependsOn(attr)
		diags = append(diags, tarsDiags...)
		opts.Target = tars
	}

	if !opts.Refresh && opts.Mode == RefreshOnlyTestMode {
		// These options are incompatible.
		diags = append(diags, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Incompatible plan options",
			Detail:   "The \"refresh\" option cannot be set to false when running a test in \"refresh-only\" mode.",
			Subject:  content.Attributes["refresh"].Range.Ptr(),
		})
	}

	return &opts, diags
}

func decodeOverrideResourceBlock(block *hcl.Block, mode addrs.ResourceMode) (*OverrideResource, hcl.Diagnostics) {
	parseTarget := func(attr *hcl.Attribute) (traversal hcl.Traversal, configRes *addrs.ConfigResource, diags hcl.Diagnostics) {
		traversal, traversalDiags := hcl.AbsTraversalForExpr(attr.Expr)
		diags = append(diags, traversalDiags...)
		if traversalDiags.HasErrors() {
			return nil, nil, diags
		}

		configRes, configResDiags := addrs.ParseConfigResource(traversal)
		diags = append(diags, configResDiags.ToHCL()...)
		if configResDiags.HasErrors() {
			return nil, nil, diags
		}

		return traversal, configRes, diags
	}

	parseValues := func(attr *hcl.Attribute) (values map[string]cty.Value, diags hcl.Diagnostics) {
		if vars := attr.Expr.Variables(); len(vars) != 0 {
			return nil, hcl.Diagnostics{
				&hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "Variables not allowed",
				Detail:   "Variables may not be used in `values` of override blocks.",
				Subject:  attr.Range.Ptr(),
				},
			}
		}

		attrVal, attrValDiags := attr.Expr.Value(nil)
		diags = append(diags, attrValDiags...)
		if attrValDiags.HasErrors() {
			return nil, diags
		}

		if !attrVal.Type().IsObjectType() {
			diags = append(diags, &hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "Object expected",
				Detail:   "Attribute `values` in override block must be an object.",
				Subject:  attr.Range.Ptr(),
			})
			return nil, diags
		}

		return attrVal.AsValueMap(), diags
		}

	res := &OverrideResource{
		Mode: mode,
	}

	content, diags := block.Body.Content(overrideResourceBlockSchema)

	if attr, exists := content.Attributes["target"]; exists {
		target, parsed, moreDiags := parseTarget(attr)
		res.Target, res.TargetParsed = target, parsed
		diags = append(diags, moreDiags...)
	}

	if attr, exists := content.Attributes["values"]; exists {
		v, moreDiags := parseValues(attr)
		res.Values, diags = v, append(diags, moreDiags...)
	}

	return res, diags
}

func decodeOverrideModuleBlock(block *hcl.Block) (*OverrideModule, hcl.Diagnostics) {
	parseTarget := func(attr *hcl.Attribute) (traversal hcl.Traversal, mod addrs.Module, diags hcl.Diagnostics) {
		traversal, traversalDiags := hcl.AbsTraversalForExpr(attr.Expr)
		diags = append(diags, traversalDiags...)
		if traversalDiags.HasErrors() {
			return nil, nil, diags
		}

		target, targetDiags := addrs.ParseModule(traversal)
		diags = append(diags, targetDiags.ToHCL()...)
		if targetDiags.HasErrors() {
			return nil, nil, diags
		}

		return traversal, target, diags
	}

	parseOutputs := func(attr *hcl.Attribute) (v map[string]cty.Value, diags hcl.Diagnostics) {
		if vars := attr.Expr.Variables(); len(vars) != 0 {
			return nil, hcl.Diagnostics{
				&hcl.Diagnostic{
					Severity: hcl.DiagError,
					Summary:  "Variables not allowed",
					Detail:   "Variables may not be used in `values` of override blocks.",
					Subject:  attr.Range.Ptr(),
				},
			}
		}

		attrVal, valDiags := attr.Expr.Value(nil)
		diags = append(diags, valDiags...)
		if valDiags.HasErrors() {
			return nil, diags
		}

		if !attrVal.Type().IsObjectType() {
			return nil, append(diags, &hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "Object expected",
				Detail:   "Attribute `values` in override block must be an object.",
				Subject:  attr.Range.Ptr(),
			})
		}

		return attrVal.AsValueMap(), diags
	}

	mod := &OverrideModule{}

	content, diags := block.Body.Content(overrideModuleBlockSchema)

	if attr, exists := content.Attributes["target"]; exists {
		traversal, target, moreDiags := parseTarget(attr)
		mod.Target, mod.TargetParsed = traversal, target
		diags = append(diags, moreDiags...)
	}

	if attr, exists := content.Attributes["outputs"]; exists {
		outputs, moreDiags := parseOutputs(attr)
		mod.Outputs, diags = outputs, append(diags, moreDiags...)
	}

	return mod, diags
}

var testFileSchema = &hcl.BodySchema{
	Blocks: []hcl.BlockHeaderSchema{
		{
			Type:       "run",
			LabelNames: []string{"name"},
		},
		{
			Type:       "provider",
			LabelNames: []string{"name"},
		},
		{
			Type: "variables",
		},
	},
}

var testRunBlockSchema = &hcl.BodySchema{
	Attributes: []hcl.AttributeSchema{
		{Name: "command"},
		{Name: "providers"},
		{Name: "expect_failures"},
	},
	Blocks: []hcl.BlockHeaderSchema{
		{
			Type: "plan_options",
		},
		{
			Type: "assert",
		},
		{
			Type: "variables",
		},
		{
			Type: "module",
		},
		{
			Type: "override_resource",
		},
		{
			Type: "override_data",
		},
		{
			Type: "override_module",
		},
	},
}

var testRunOptionsBlockSchema = &hcl.BodySchema{
	Attributes: []hcl.AttributeSchema{
		{Name: "mode"},
		{Name: "refresh"},
		{Name: "replace"},
		{Name: "target"},
	},
}

var testRunModuleBlockSchema = &hcl.BodySchema{
	Attributes: []hcl.AttributeSchema{
		{Name: "source"},
		{Name: "version"},
	},
}

var overrideResourceBlockSchema = &hcl.BodySchema{
	Attributes: []hcl.AttributeSchema{
		{
			Name:     "target",
			Required: true,
		},
		{
			Name:     "values",
			Required: false,
		},
	},
}

var overrideModuleBlockSchema = &hcl.BodySchema{
	Attributes: []hcl.AttributeSchema{
		{
			Name:     "target",
			Required: true,
		},
		{
			Name:     "outputs",
			Required: false,
		},
	},
}
