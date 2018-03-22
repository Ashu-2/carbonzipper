package helper

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"sync/atomic"

	"github.com/go-graphite/carbonzipper/limiter"
	cu "github.com/go-graphite/carbonzipper/util/apictx"
	util "github.com/go-graphite/carbonzipper/util/zipperctx"
	"github.com/go-graphite/carbonzipper/zipper/types"
	"go.uber.org/zap"
)

type ServerResponse struct {
	Server   string
	Response []byte
}

type HttpQuery struct {
	groupName string
	servers   []string
	maxTries  int
	logger    *zap.Logger
	limiter   limiter.ServerLimiter
	client    *http.Client

	counter uint64
}

func NewHttpQuery(logger *zap.Logger, groupName string, servers []string, maxTries int, limiter limiter.ServerLimiter, client *http.Client) *HttpQuery {
	return &HttpQuery{
		groupName: groupName,
		servers:   servers,
		maxTries:  maxTries,
		logger:    logger.With(zap.String("action", "query")),
		limiter:   limiter,
		client:    client,
	}
}

func (c *HttpQuery) pickServer() string {
	if len(c.servers) == 1 {
		// No need to do heavy operations here
		return c.servers[0]
	}
	logger := c.logger.With(zap.String("function", "picker"))
	counter := atomic.AddUint64(&(c.counter), 1)
	idx := counter % uint64(len(c.servers))
	srv := c.servers[int(idx)]
	logger.Debug("picked",
		zap.Uint64("counter", counter),
		zap.Uint64("idx", idx),
		zap.String("Server", srv),
	)

	return srv
}

func (c *HttpQuery) doRequest(ctx context.Context, uri string) (*ServerResponse, error) {
	server := c.pickServer()

	u, err := url.Parse(server + uri)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return nil, err
	}
	req = cu.MarshalCtx(ctx, util.MarshalCtx(ctx, req))

	c.logger.Debug("trying to get slot",
		zap.String("name", c.groupName),
		zap.String("uri", u.String()),
	)

	err = c.limiter.Enter(ctx, c.groupName)
	if err != nil {
		c.logger.Debug("timeout waiting for a slot")
		return nil, err
	}
	defer c.limiter.Leave(ctx, server)

	c.logger.Debug("got slot")

	resp, err := c.client.Do(req.WithContext(ctx))
	if err != nil {
		c.logger.Error("error fetching result",
			zap.Error(err),
		)
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		c.logger.Error("status not ok, not found",
			zap.Int("status_code", resp.StatusCode),
		)
		return nil, types.ErrNotFound
	}

	if resp.StatusCode != http.StatusOK {
		c.logger.Error("status not ok",
			zap.Int("status_code", resp.StatusCode),
		)
		return nil, fmt.Errorf(types.ErrFailedToFetchFmt, c.groupName, resp.StatusCode)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		c.logger.Error("error reading body",
			zap.Error(err),
		)
		return nil, err
	}

	return &ServerResponse{Server: server, Response: body}, nil
}

func (c *HttpQuery) DoQuery(ctx context.Context, uri string) (*ServerResponse, error) {
	maxTries := c.maxTries
	if len(c.servers) > maxTries {
		maxTries = len(c.servers)
	}
	var res *ServerResponse
	var err error
	for try := 0; try < maxTries; try++ {
		res, err = c.doRequest(ctx, uri)
		if err != nil {
			if err == types.ErrNotFound {
				return nil, err
			}
			continue
		}

		return res, nil
	}

	return nil, err
}
