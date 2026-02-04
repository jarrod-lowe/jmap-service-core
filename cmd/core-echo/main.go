package main

import (
	"context"
	"log/slog"

	"github.com/jarrod-lowe/jmap-service-core/pkg/plugincontract"
	"github.com/jarrod-lowe/jmap-service-libs/awsinit"
	"github.com/jarrod-lowe/jmap-service-libs/logging"
)

var logger = logging.New()

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
	ctx := context.Background()

	result, err := awsinit.Init(ctx)
	if err != nil {
		logger.Error("FATAL: Failed to initialize AWS",
			slog.String("error", err.Error()),
		)
		panic(err)
	}
	defer result.Cleanup()

	result.Start(handler)
}
