// Package mcpbridge is `canopy mcp`: an MCP server on stdio, started by the CLI inside the
// sandbox, that passes each tool call to the runner over a Unix socket.
package mcpbridge

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"sync/atomic"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"canopy/client/internal/tools"
)

type bridge struct {
	conn    net.Conn
	wmu     sync.Mutex
	pmu     sync.Mutex
	pending map[string]chan tools.Result
	seq     atomic.Uint64
}

func Run(ctx context.Context, socket, version string) error {
	c, err := net.Dial("unix", socket)
	if err != nil {
		return fmt.Errorf("connect to the Canopy runner: %w", err)
	}
	b := &bridge{conn: c, pending: map[string]chan tools.Result{}}
	go b.read()

	server := mcp.NewServer(&mcp.Implementation{Name: "canopy", Version: version}, nil)
	for _, d := range tools.Defs {
		var schema map[string]any
		if err := json.Unmarshal([]byte(d.Schema), &schema); err != nil {
			return fmt.Errorf("tool %s schema: %w", d.Name, err)
		}
		name := d.Name
		server.AddTool(&mcp.Tool{Name: d.Name, Description: d.Description, InputSchema: schema},
			func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				args := json.RawMessage(req.Params.Arguments)
				if len(args) == 0 {
					args = json.RawMessage("{}")
				}
				res, err := b.call(ctx, name, args)
				if err != nil {
					return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}, IsError: true}, nil
				}
				return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: res.Text}}, IsError: res.IsError}, nil
			})
	}
	return server.Run(ctx, &mcp.StdioTransport{})
}

func (b *bridge) call(ctx context.Context, tool string, args json.RawMessage) (tools.Result, error) {
	id := fmt.Sprintf("c%d", b.seq.Add(1))
	ch := make(chan tools.Result, 1)
	b.pmu.Lock()
	b.pending[id] = ch
	b.pmu.Unlock()
	defer func() {
		b.pmu.Lock()
		delete(b.pending, id)
		b.pmu.Unlock()
	}()
	line, _ := json.Marshal(tools.Call{CallID: id, Tool: tool, Args: args})
	b.wmu.Lock()
	_, err := b.conn.Write(append(line, '\n'))
	b.wmu.Unlock()
	if err != nil {
		return tools.Result{}, fmt.Errorf("the Canopy runner is unavailable: %w", err)
	}
	select {
	case r, ok := <-ch:
		if !ok {
			return tools.Result{}, fmt.Errorf("the Canopy runner went away")
		}
		return r, nil
	case <-ctx.Done():
		return tools.Result{}, ctx.Err()
	}
}

func (b *bridge) read() {
	sc := bufio.NewScanner(b.conn)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		var r tools.Result
		if json.Unmarshal(sc.Bytes(), &r) != nil {
			continue
		}
		b.pmu.Lock()
		ch := b.pending[r.CallID]
		b.pmu.Unlock()
		if ch != nil {
			ch <- r
		}
	}
	b.pmu.Lock()
	for id, ch := range b.pending {
		close(ch)
		delete(b.pending, id)
	}
	b.pmu.Unlock()
}
