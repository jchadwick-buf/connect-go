// Copyright 2021-2022 Buf Technologies, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Code generated by protoc-gen-connect-go. DO NOT EDIT.
//
// Source: grpc/reflection/v1alpha/reflection.proto

package reflectionv1alpha1

import (
	context "context"
	errors "errors"
	connect "github.com/bufbuild/connect"
	v1alpha "github.com/bufbuild/connect/internal/gen/proto/go/grpc/reflection/v1alpha"
	http "net/http"
	strings "strings"
)

// This is a compile-time assertion to ensure that this generated file and the connect package are
// compatible. If you get a compiler error that this constant isn't defined, this code was generated
// with a version of connect newer than the one compiled into your binary. You can fix the problem
// by either regenerating this code with an older version of connect or updating the connect version
// compiled into your binary.
const _ = connect.IsAtLeastVersion0_0_1

// ServerReflectionClient is a client for the internal.reflection.v1alpha1.ServerReflection service.
type ServerReflectionClient interface {
	// The reflection service is structured as a bidirectional stream, ensuring
	// all related requests go to a single server.
	ServerReflectionInfo(context.Context) *connect.BidiStreamForClient[v1alpha.ServerReflectionRequest, v1alpha.ServerReflectionResponse]
}

// NewServerReflectionClient constructs a client for the
// internal.reflection.v1alpha1.ServerReflection service. By default, it uses the binary protobuf
// Codec, asks for gzipped responses, and sends uncompressed requests. It doesn't have a default
// protocol; you must supply either the connect.WithGRPC() or connect.WithGRPCWeb() options.
//
// The URL supplied here should be the base URL for the gRPC server (e.g., https://api.acme.com or
// https://acme.com/grpc).
func NewServerReflectionClient(baseURL string, doer connect.Doer, opts ...connect.ClientOption) (ServerReflectionClient, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	serverReflectionInfoClient, err := connect.NewClient[v1alpha.ServerReflectionRequest, v1alpha.ServerReflectionResponse](
		baseURL+"/internal.reflection.v1alpha1.ServerReflection/ServerReflectionInfo",
		doer,
		opts...,
	)
	if err != nil {
		return nil, err
	}
	return &serverReflectionClient{
		serverReflectionInfo: serverReflectionInfoClient,
	}, nil
}

// serverReflectionClient implements ServerReflectionClient.
type serverReflectionClient struct {
	serverReflectionInfo *connect.Client[v1alpha.ServerReflectionRequest, v1alpha.ServerReflectionResponse]
}

// ServerReflectionInfo calls internal.reflection.v1alpha1.ServerReflection.ServerReflectionInfo.
func (c *serverReflectionClient) ServerReflectionInfo(ctx context.Context) *connect.BidiStreamForClient[v1alpha.ServerReflectionRequest, v1alpha.ServerReflectionResponse] {
	return c.serverReflectionInfo.CallBidiStream(ctx)
}

// ServerReflectionHandler is an implementation of the internal.reflection.v1alpha1.ServerReflection
// service.
type ServerReflectionHandler interface {
	// The reflection service is structured as a bidirectional stream, ensuring
	// all related requests go to a single server.
	ServerReflectionInfo(context.Context, *connect.BidiStream[v1alpha.ServerReflectionRequest, v1alpha.ServerReflectionResponse]) error
}

// NewServerReflectionHandler builds an HTTP handler from the service implementation. It returns the
// path on which to mount the handler and the handler itself.
//
// By default, handlers support the gRPC and gRPC-Web protocols with the binary protobuf and JSON
// codecs.
func NewServerReflectionHandler(svc ServerReflectionHandler, opts ...connect.HandlerOption) (string, http.Handler) {
	mux := http.NewServeMux()
	mux.Handle("/internal.reflection.v1alpha1.ServerReflection/ServerReflectionInfo", connect.NewBidiStreamHandler(
		"/internal.reflection.v1alpha1.ServerReflection/ServerReflectionInfo",
		svc.ServerReflectionInfo,
		opts...,
	))
	return "/internal.reflection.v1alpha1.ServerReflection/", mux
}

// UnimplementedServerReflectionHandler returns CodeUnimplemented from all methods.
type UnimplementedServerReflectionHandler struct{}

func (UnimplementedServerReflectionHandler) ServerReflectionInfo(context.Context, *connect.BidiStream[v1alpha.ServerReflectionRequest, v1alpha.ServerReflectionResponse]) error {
	return connect.NewError(connect.CodeUnimplemented, errors.New("internal.reflection.v1alpha1.ServerReflection.ServerReflectionInfo isn't implemented"))
}
