// Code generated by protoc-gen-go-grpc. DO NOT EDIT.

package api

import (
	context "context"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
const _ = grpc.SupportPackageIsVersion7

// AnalyticsServerClient is the client API for AnalyticsServer service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type AnalyticsServerClient interface {
	// Pushes analytics data to the server
	PushMetrics(ctx context.Context, in *AnalyticsMetrics, opts ...grpc.CallOption) (*ServiceResponse, error)
}

type analyticsServerClient struct {
	cc grpc.ClientConnInterface
}

func NewAnalyticsServerClient(cc grpc.ClientConnInterface) AnalyticsServerClient {
	return &analyticsServerClient{cc}
}

func (c *analyticsServerClient) PushMetrics(ctx context.Context, in *AnalyticsMetrics, opts ...grpc.CallOption) (*ServiceResponse, error) {
	out := new(ServiceResponse)
	err := c.cc.Invoke(ctx, "/api.AnalyticsServer/PushMetrics", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// AnalyticsServerServer is the server API for AnalyticsServer service.
// All implementations must embed UnimplementedAnalyticsServerServer
// for forward compatibility
type AnalyticsServerServer interface {
	// Pushes analytics data to the server
	PushMetrics(context.Context, *AnalyticsMetrics) (*ServiceResponse, error)
	mustEmbedUnimplementedAnalyticsServerServer()
}

// UnimplementedAnalyticsServerServer must be embedded to have forward compatible implementations.
type UnimplementedAnalyticsServerServer struct {
}

func (UnimplementedAnalyticsServerServer) PushMetrics(context.Context, *AnalyticsMetrics) (*ServiceResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method PushMetrics not implemented")
}
func (UnimplementedAnalyticsServerServer) mustEmbedUnimplementedAnalyticsServerServer() {}

// UnsafeAnalyticsServerServer may be embedded to opt out of forward compatibility for this service.
// Use of this interface is not recommended, as added methods to AnalyticsServerServer will
// result in compilation errors.
type UnsafeAnalyticsServerServer interface {
	mustEmbedUnimplementedAnalyticsServerServer()
}

func RegisterAnalyticsServerServer(s grpc.ServiceRegistrar, srv AnalyticsServerServer) {
	s.RegisterService(&_AnalyticsServer_serviceDesc, srv)
}

func _AnalyticsServer_PushMetrics_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(AnalyticsMetrics)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(AnalyticsServerServer).PushMetrics(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/api.AnalyticsServer/PushMetrics",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(AnalyticsServerServer).PushMetrics(ctx, req.(*AnalyticsMetrics))
	}
	return interceptor(ctx, in, info, handler)
}

var _AnalyticsServer_serviceDesc = grpc.ServiceDesc{
	ServiceName: "api.AnalyticsServer",
	HandlerType: (*AnalyticsServerServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "PushMetrics",
			Handler:    _AnalyticsServer_PushMetrics_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "api.proto",
}