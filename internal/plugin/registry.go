package plugin

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// PluginPrefix is the partition key prefix for plugin records
const PluginPrefix = "PLUGIN#"

// CoreCapability is the URN for the core JMAP capability
const CoreCapability = "urn:ietf:params:jmap:core"

// PluginQuerier defines the interface for querying plugins from storage
type PluginQuerier interface {
	QueryByPK(ctx context.Context, pk string) ([]map[string]types.AttributeValue, error)
}

// Registry holds loaded plugin configuration
type Registry struct {
	methodMap        map[string]MethodTarget
	capabilitySet    map[string]bool
	capabilityConfig map[string]map[string]any
	plugins          []PluginRecord
}

// NewRegistry creates a registry with built-in core capability
func NewRegistry() *Registry {
	return &Registry{
		methodMap: make(map[string]MethodTarget),
		capabilitySet: map[string]bool{
			CoreCapability: true, // Core is always available
		},
		capabilityConfig: make(map[string]map[string]any),
		plugins:          []PluginRecord{},
	}
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
		for method, target := range record.Methods {
			r.methodMap[method] = target
		}

		// Index capabilities
		for capability, config := range record.Capabilities {
			r.capabilitySet[capability] = true
			r.capabilityConfig[capability] = config
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
