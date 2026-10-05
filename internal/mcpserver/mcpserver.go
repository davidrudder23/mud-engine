// Package mcpserver exposes the game as MCP tools -- one per command --
// so an MCP-capable client (Claude Desktop, Claude Code, etc.) can drive
// a single Player as a game session, instead of a human over telnet.
//
// One player per server: there's no login flow the way telnet has one.
// The player name comes from config at startup (MUD_MCP_PLAYER_NAME, in
// cmd/mcp-server), resolved via telnet.ResolvePlayer so a first-ever
// run, a restart, and a saved-room-that-no-longer-exists are all
// handled identically to a telnet login. commands.BuildAll builds the
// exact same commands a telnet connection gets, bound to this session.
//
// Every tool takes exactly the parameters its command declares (all
// strings) and returns the same shape:
//
//	{"message": "<RunCommand's own text>", "room": {...Room.View()...}}
//
// "message" is whatever the command itself would say; "room" is a fresh,
// structured snapshot of wherever the player ends up standing, on every
// call -- so nothing extra needs calling just to see what changed. The
// same JSON is also sent as a text content block, for clients that only
// read those.
package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"mudengine/internal/combat"
	"mudengine/internal/commands"
	"mudengine/internal/llm"
	"mudengine/internal/logging"
	"mudengine/internal/repository"
	"mudengine/internal/session"
	"mudengine/internal/telnet"
	"mudengine/internal/world"
)

var logger = logging.For("mud.mcpserver")

// ToolName is command's MCP tool name. MCP tool names are identifiers
// -- "pick up" isn't one -- so spaces become underscores ("pick_up").
// Purely a presentation-layer rename; the command's own Name keeps its
// space.
func ToolName(c commands.Command) string {
	return strings.ReplaceAll(c.Name(), " ", "_")
}

// inputSchema is the JSON schema for c's parameters: one string
// property per parameter, the required ones listed as required.
func inputSchema(c commands.Command) map[string]any {
	properties := map[string]any{}
	var required []string
	for _, p := range c.Parameters() {
		properties[p.Name] = map[string]any{"type": string(p.Type)}
		if p.Required {
			required = append(required, p.Name)
		}
	}
	schema := map[string]any{"type": "object", "properties": properties}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

// Result is what every tool call returns.
type Result struct {
	Message string         `json:"message"`
	Room    world.RoomView `json:"room"`
}

func errorResult(format string, args ...any) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(format, args...)}},
		IsError: true,
	}
}

// Options customizes BuildServer. Every field is optional.
type Options struct {
	// How `talk` gets its LLM client (nil for llm.GetClient).
	TalkLLM func() (llm.Client, error)
}

// BuildServer resolves playerName into a Player (creating it if
// needed), places it in the world, and builds an MCP server with one
// tool per command.
func BuildServer(ctx context.Context, w *world.World, players repository.PlayerRepository, playerName string, opts Options) (*mcp.Server, error) {
	player, err := telnet.ResolvePlayer(ctx, w, players, playerName)
	if err != nil {
		return nil, err
	}
	if err := w.PlacePlayer(player); err != nil {
		return nil, err
	}

	sessions := session.NewRegistry()
	sess := sessions.Create(player)
	all := commands.BuildAll(sessions, w, players, opts.TalkLLM)

	server := mcp.NewServer(
		&mcp.Implementation{Name: "mud-engine", Version: "0.1.0"},
		&mcp.ServerOptions{Instructions: "You are playing a text-based multiplayer dungeon (MUD) as " +
			player.Name + ". Each tool call runs one in-game command and " +
			"returns {message, room}: `message` is that command's own " +
			"reply, `room` is a fresh, structured snapshot of your " +
			"current surroundings -- read it after every call instead " +
			"of assuming what changed."},
	)

	// Tool calls can arrive concurrently; the game logic assumes one
	// at a time, same as the telnet server's game lock.
	var lock sync.Mutex
	// Stamina comes back over time, for as long as ctx lives.
	go combat.RunRegen(ctx, w, &lock)
	for _, command := range all {
		tool := &mcp.Tool{
			Name:        ToolName(command),
			Description: commands.CollapseWhitespace(command.Describe()),
			InputSchema: inputSchema(command),
		}
		server.AddTool(tool, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args map[string]any
			if len(req.Params.Arguments) > 0 {
				if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
					return errorResult("invalid arguments: %v", err), nil
				}
			}
			values := map[string]string{}
			for _, p := range command.Parameters() {
				raw, present := args[p.Name]
				if !present {
					if p.Required {
						return errorResult("missing required argument %q", p.Name), nil
					}
					continue
				}
				value, ok := raw.(string)
				if !ok {
					return errorResult("argument %q must be a string", p.Name), nil
				}
				values[p.Name] = value
			}

			lock.Lock()
			defer lock.Unlock()
			message, err := command.RunCommand(ctx, commands.BuildLine(command, values), sess.ID)
			if err != nil {
				return errorResult("%v", err), nil
			}
			room, err := w.GetRoom(sess.Player.CurrentRoomID)
			if err != nil {
				return errorResult("%v", err), nil
			}

			result := Result{Message: message, Room: room.View()}
			text, err := json.Marshal(result)
			if err != nil {
				return nil, err
			}
			return &mcp.CallToolResult{
				Content:           []mcp.Content{&mcp.TextContent{Text: string(text)}},
				StructuredContent: result,
			}, nil
		})
	}

	logger.Info("MCP server built", "player", player.Name, "room", player.CurrentRoomID, "tools", len(all))
	return server, nil
}
