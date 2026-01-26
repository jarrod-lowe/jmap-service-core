package plugin

import (
	"context"
	"fmt"
	"maps"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// PluginPrefix is the partition key prefix for plugin records
const PluginPrefix = "PLUGIN#"

// PluginQuerier defines the interface for querying plugins from storage
type PluginQuerier interface {
	QueryByPK(ctx context.Context, pk string) ([]map[string]types.AttributeValue, error)
}

// Registry holds loaded plugin configuration
type Registry struct {
	methodMap         map[string]MethodTarget
	capabilitySet     map[string]bool
	capabilityConfig  map[string]map[string]any
	plugins           []PluginRecord
	allowedPrincipals map[string]bool // aggregated from all plugins' ClientPrincipals
}

// NewRegistry creates an empty registry
func NewRegistry() *Registry {
	return &Registry{
		methodMap:         make(map[string]MethodTarget),
		capabilitySet:     make(map[string]bool),
		capabilityConfig:  make(map[string]map[string]any),
		plugins:           []PluginRecord{},
		allowedPrincipals: make(map[string]bool),
	}
}

// NewRegistryWithPrincipals creates a registry with pre-populated allowed principals.
// This is primarily for testing.
func NewRegistryWithPrincipals(principals []string) *Registry {
	r := NewRegistry()
	for _, p := range principals {
		r.allowedPrincipals[p] = true
	}
	return r
}

// LoadFromDynamoDB loads all plugins from DynamoDB
func (r *Registry) LoadFromDynamoDB(ctx context.Context, querier PluginQuerier) error {
	items, err := querier.QueryByPK(ctx, PluginPrefix)
	if err != nil {
		return fmt.Errorf("failed to query plugins: %w", err)
	}

	for _, item := range items {
		var record PluginRecord
		if err := attributevalue.UnmarshalMap(item, &record); err != nil {
			return fmt.Errorf("failed to unmarshal plugin record: %w", err)
		}

		r.plugins = append(r.plugins, record)

		// Index methods
		maps.Copy(r.methodMap, record.Methods)

		// Index capabilities with merging
		for capability, config := range record.Capabilities {
			r.capabilitySet[capability] = true
			if existing, ok := r.capabilityConfig[capability]; ok {
				// Merge: new config values overwrite existing
				maps.Copy(existing, config)
			} else {
				// Make a copy to avoid aliasing
				r.capabilityConfig[capability] = maps.Clone(config)
			}
		}

		// Aggregate client principals
		for _, principal := range record.ClientPrincipals {
			r.allowedPrincipals[principal] = true
		}
	}

	return nil
}

// GetMethodTarget returns the target for a method, or nil if not found
func (r *Registry) GetMethodTarget(method string) *MethodTarget {
	target, ok := r.methodMap[method]
	if !ok {
		return nil
	}
	return &target
}

// GetCapabilities returns all available capability URNs
func (r *Registry) GetCapabilities() []string {
	caps := make([]string, 0, len(r.capabilitySet))
	for cap := range r.capabilitySet {
		caps = append(caps, cap)
	}
	return caps
}

// GetCapabilityConfig returns the merged configuration for a capability
func (r *Registry) GetCapabilityConfig(capability string) map[string]any {
	config, ok := r.capabilityConfig[capability]
	if !ok {
		return nil
	}
	return config
}

// HasCapability checks if a capability is available
func (r *Registry) HasCapability(capability string) bool {
	return r.capabilitySet[capability]
}

// IsAllowedPrincipal checks if the given caller ARN is allowed to access IAM endpoints.
// Returns true if the caller is registered by any plugin.
// Handles assumed-role ARN translation automatically.
func (r *Registry) IsAllowedPrincipal(callerARN string) bool {
	// Convert map keys to slice for IsAllowedARN
	registeredARNs := make([]string, 0, len(r.allowedPrincipals))
	for arn := range r.allowedPrincipals {
		registeredARNs = append(registeredARNs, arn)
	}
	return IsAllowedARN(registeredARNs, callerARN)
}

// AddMethod adds a method target to the registry.
// This is primarily for testing.
func (r *Registry) AddMethod(method string, target MethodTarget) {
	r.methodMap[method] = target
}
