package plugin

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
)

// Invoker defines the interface for invoking plugin methods
type Invoker interface {
	Invoke(ctx context.Context, target MethodTarget, request PluginInvocationRequest) (*PluginInvocationResponse, error)
}

// LambdaClient defines the interface for Lambda operations
type LambdaClient interface {
	Invoke(ctx context.Context, params *lambda.InvokeInput, optFns ...func(*lambda.Options)) (*lambda.InvokeOutput, error)
}

// LambdaInvoker invokes plugins via AWS Lambda
type LambdaInvoker struct {
	client LambdaClient
}

// NewLambdaInvoker creates a new Lambda invoker
func NewLambdaInvoker(client LambdaClient) *LambdaInvoker {
	return &LambdaInvoker{client: client}
}

// Invoke invokes a plugin Lambda with the given request
func (i *LambdaInvoker) Invoke(ctx context.Context, target MethodTarget, request PluginInvocationRequest) (*PluginInvocationResponse, error) {
	// Marshal request to JSON
	payload, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Invoke Lambda
	input := &lambda.InvokeInput{
		FunctionName: aws.String(target.InvokeTarget),
		Payload:      payload,
	}

	output, err := i.client.Invoke(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("lambda invocation failed: %w", err)
	}

	// Unmarshal response
	var response PluginInvocationResponse
	if err := json.Unmarshal(output.Payload, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &response, nil
}
