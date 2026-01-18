package main

import (
	"context"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/jarrod-lowe/jmap-service-core/pkg/plugincontract"
)

// handler is the Lambda handler for Core/echo
// Per RFC 8620 Section 3.5, this method echoes back the arguments unchanged
func handler(ctx context.Context, request plugincontract.PluginInvocationRequest) (plugincontract.PluginInvocationResponse, error) {
	return plugincontract.PluginInvocationResponse{
		MethodResponse: plugincontract.MethodResponse{
			Name:     "Core/echo",
			Args:     request.Args,
			ClientID: request.ClientID,
		},
	}, nil
}

func main() {
	lambda.Start(handler)
}
