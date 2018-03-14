package grpc

import (
	"context"
	"fmt"
	"math"

	"github.com/go-graphite/carbonzipper/limiter"
	"github.com/go-graphite/carbonzipper/zipper/metadata"
	"github.com/go-graphite/carbonzipper/zipper/types"
	protov3grpc "github.com/go-graphite/protocol/carbonapi_v3_grpc"
	protov3 "github.com/go-graphite/protocol/carbonapi_v3_pb"
	"google.golang.org/grpc"
	_ "google.golang.org/grpc/balancer/roundrobin"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"

	"github.com/lomik/zapwriter"
	"go.uber.org/zap"
)

func init() {
	aliases := []string{"carbonapi_v3_grpc", "proto_v3_grpc", "v3_grpc"}
	metadata.Metadata.Lock()
	for _, name := range aliases {
		metadata.Metadata.SupportedProtocols[name] = struct{}{}
		metadata.Metadata.ProtocolInits[name] = NewClientGRPCGroup
		metadata.Metadata.ProtocolInitsWithLimiter[name] = NewClientGRPCGroupWithLimiter
	}
	defer metadata.Metadata.Unlock()
}

// RoundRobin is used to connect to backends inside clientGRPCGroups, implements ServerClient interface
type ClientGRPCGroup struct {
	groupName string
	servers   []string

	r        *manual.Resolver
	conn     *grpc.ClientConn
	dialerrc chan error
	cleanup  func()
	timeout  types.Timeouts

	client protov3grpc.CarbonV1Client
}

func NewClientGRPCGroupWithLimiter(config types.BackendV2, limiter limiter.ServerLimiter) (types.ServerClient, error) {
	return NewClientGRPCGroup(config)
}

func NewClientGRPCGroup(config types.BackendV2) (types.ServerClient, error) {
	// TODO: Implement normal resolver
	if len(config.Servers) == 0 {
		return nil, fmt.Errorf("no servers specified")
	}
	r, cleanup := manual.GenerateAndRegisterManualResolver()
	var resolvedAddrs []resolver.Address
	for _, addr := range config.Servers {
		resolvedAddrs = append(resolvedAddrs, resolver.Address{Addr: addr})
	}

	r.NewAddress(resolvedAddrs)

	opts := []grpc.DialOption{
		grpc.WithUserAgent("carbonzipper"),
		grpc.WithCompressor(grpc.NewGZIPCompressor()),
		grpc.WithDecompressor(grpc.NewGZIPDecompressor()),
		grpc.WithBalancerName("roundrobin"), // TODO: Make that configurable
		grpc.WithMaxMsgSize(math.MaxUint32), // TODO: make that configurable
		grpc.WithInsecure(),                 // TODO: Make configurable
	}

	conn, err := grpc.Dial(r.Scheme()+":///server", opts...)
	if err != nil {
		cleanup()
		return nil, err
	}

	client := &ClientGRPCGroup{
		groupName: config.GroupName,
		servers:   config.Servers,

		r:       r,
		cleanup: cleanup,
		conn:    conn,
		client:  protov3grpc.NewCarbonV1Client(conn),
		timeout: *config.Timeouts,
	}

	return client, nil
}

func (c ClientGRPCGroup) Name() string {
	return c.groupName
}

func (c ClientGRPCGroup) Backends() []string {
	return c.servers
}

func (c *ClientGRPCGroup) Fetch(ctx context.Context, request *protov3.MultiFetchRequest) (*protov3.MultiFetchResponse, *types.Stats, error) {
	stats := &types.Stats{
		Servers: []string{c.Name()},
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout.Render)
	defer cancel()

	res, err := c.client.FetchMetrics(ctx, request)
	if err != nil {
		stats.RenderErrors++
		stats.FailedServers = stats.Servers
		stats.Servers = []string{}
	}
	stats.MemoryUsage = int64(res.Size())

	return res, stats, err
}

func (c *ClientGRPCGroup) Find(ctx context.Context, request *protov3.MultiGlobRequest) (*protov3.MultiGlobResponse, *types.Stats, error) {
	stats := &types.Stats{
		Servers: []string{c.Name()},
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout.Find)
	defer cancel()

	res, err := c.client.FindMetrics(ctx, request)
	if err != nil {
		stats.RenderErrors++
		stats.FailedServers = stats.Servers
		stats.Servers = []string{}
	}
	stats.MemoryUsage = int64(res.Size())

	return res, stats, err
}
func (c *ClientGRPCGroup) Info(ctx context.Context, request *protov3.MultiMetricsInfoRequest) (*protov3.ZipperInfoResponse, *types.Stats, error) {
	stats := &types.Stats{
		Servers: []string{c.Name()},
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout.Render)
	defer cancel()

	res, err := c.client.MetricsInfo(ctx, request)
	if err != nil {
		stats.RenderErrors++
		stats.FailedServers = stats.Servers
		stats.Servers = []string{}
	}
	stats.MemoryUsage = int64(res.Size())

	r := &protov3.ZipperInfoResponse{
		Info: map[string]protov3.MultiMetricsInfoResponse{
			c.Name(): *res,
		},
	}

	return r, stats, err
}

func (c *ClientGRPCGroup) List(ctx context.Context) (*protov3.ListMetricsResponse, *types.Stats, error) {
	stats := &types.Stats{
		Servers: []string{c.Name()},
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout.Render)
	defer cancel()

	res, err := c.client.ListMetrics(ctx, types.EmptyMsg)
	if err != nil {
		stats.RenderErrors++
		stats.FailedServers = stats.Servers
		stats.Servers = []string{}
	}
	stats.MemoryUsage = int64(res.Size())

	return res, stats, err
}
func (c *ClientGRPCGroup) Stats(ctx context.Context) (*protov3.MetricDetailsResponse, *types.Stats, error) {
	stats := &types.Stats{
		Servers: []string{c.Name()},
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout.Render)
	defer cancel()

	res, err := c.client.Stats(ctx, types.EmptyMsg)
	if err != nil {
		stats.RenderErrors++
		stats.FailedServers = stats.Servers
		stats.Servers = []string{}
	}
	stats.MemoryUsage = int64(res.Size())

	return res, stats, err
}

func (c *ClientGRPCGroup) ProbeTLDs(ctx context.Context) ([]string, error) {
	logger := zapwriter.Logger("probe").With(zap.String("groupName", c.groupName))

	ctx, cancel := context.WithTimeout(ctx, c.timeout.Find)
	defer cancel()

	req := &protov3.MultiGlobRequest{
		Metrics: []string{"*"},
	}

	logger.Debug("doing request",
		zap.Any("request", req),
	)

	res, _, err := c.Find(ctx, req)
	if err != nil {
		return nil, err
	}
	var tlds []string
	for _, m := range res.Metrics {
		for _, v := range m.Matches {
			tlds = append(tlds, v.Path)
		}
	}
	return tlds, nil
}
