package main

import (
	"github.com/hasura/ndc-sdk-go/connector"
	"hasura-ndc.dev/ndc-go/types"
)

// Start the connector server at http://localhost:8080
//
//	go run . serve
//
// See [NDC Go SDK] for more information.
//
// [NDC Go SDK]: https://github.com/hasura/ndc-sdk-go
func main() {
	if err := connector.Start[types.Configuration, types.State](
		&Connector{},
		connector.WithMetricsPrefix("ndc_go"),
		connector.WithDefaultServiceName("ndc_go"),
	); err != nil {
		panic(err)
	}
}
