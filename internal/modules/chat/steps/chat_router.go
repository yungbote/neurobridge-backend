package steps

import (
    "context"
    "encoding/json"
    "os"
    "strings"

    "github.com/google/uuid"

    types "github.com/yungbote/neurobridge-backend/internal/domain"
)

type chatRouteDecision struct {
    Route       string          `json:"route"`
    RespondFast bool            `json:"respond_fast"`
    ToolCalls   []chatToolCall  `json:"tool_calls"`
}

type chatToolCall struct {
    ToolName   string                 `json:"tool_name"`
    Arguments  map[string]any         `json:"arguments"`
    Confidence float64                `json:"confidence"`
}

func resolveChatRouteModel() string {
    return strings.TrimSpace(os.Getenv("CHAT_ROUTE_MODEL"))
}

func resolveChatFastModel() string {
    return strings.TrimSpace(os.Getenv("CHAT_FAST_MODEL"))
}

func routeChatMessage(ctx context.Context, deps RespondDeps, thread *types.ChatThread, userText string, recent string) (chatRouteDecision, error) {
    out := chatRouteDecision{Route: "product"}
    if deps.AI == nil || thread == nil {
        return out, nil
    }
    userText = strings.TrimSpace(userText)
    if userText == "" {
        return out, nil
    }

    tools := allowedChatTools()
    toolJSON := "[]"
    if b, err := json.Marshal(tools); err == nil {
        toolJSON = string(b)
    }

    pathID := ""
    if thread.PathID != nil && *thread.PathID != uuid.Nil {
        pathID = thread.PathID.String()
    }

    system := strings.TrimSpace(strings.Join([]string{
        "You route user messages for a learning product.",
        "Choose one route:",
        "- tool: user explicitly asks to trigger a pipeline (build, reindex, rebuild)",
        "- product: questions about learning content, paths, materials, progress, or the app",
        "- smalltalk: off-topic or casual chat unrelated to learning",
        "If unsure, choose product.",
        "If route=tool, include at most 1 tool call from the allowed list.",
        "Return ONLY JSON matching the schema.",
    }, "\n"))

    user := strings.TrimSpace(strings.Join([]string{
        "THREAD_PATH_ID: " + defaultString(pathID, "(none)"),
        "RECENT_MESSAGES:",
        defaultString(recent, "(none)"),
        "", 
        "USER_MESSAGE:",
        userText,
        "",
        "ALLOWED_TOOLS:",
        toolJSON,
    }, "\n"))

    schema := map[string]any{
        "type":                 "object",
        "additionalProperties": false,
        "properties": map[string]any{
            "route": map[string]any{
                "type": "string",
                "enum": []any{"tool", "product", "smalltalk"},
            },
            "respond_fast": map[string]any{"type": "boolean"},
            "tool_calls": map[string]any{
                "type": "array",
                "items": map[string]any{
                    "type":                 "object",
                    "additionalProperties": false,
                    "properties": map[string]any{
                        "tool_name": map[string]any{"type": "string", "enum": toolNamesForSchema()},
                        "arguments": map[string]any{"type": "object"},
                        "confidence": map[string]any{"type": "number", "minimum": 0, "maximum": 1},
                    },
                    "required": []any{"tool_name", "arguments", "confidence"},
                },
            },
        },
        "required": []any{"route", "respond_fast", "tool_calls"},
    }

    obj, err := deps.AI.GenerateJSON(ctx, system, user, "chat_route_v1", schema)
    if err != nil {
        return out, err
    }
    b, _ := json.Marshal(obj)
    _ = json.Unmarshal(b, &out)
    out.Route = strings.TrimSpace(strings.ToLower(out.Route))
    if out.Route == "" {
        out.Route = "product"
    }
    return out, nil
}

func toolNamesForSchema() []any {
    tools := allowedChatTools()
    out := make([]any, 0, len(tools))
    for _, t := range tools {
        out = append(out, t.Name)
    }
    return out
}

type chatToolSpec struct {
    Name        string `json:"name"`
    Description string `json:"description"`
    Required    []string `json:"required_args"`
    Optional    []string `json:"optional_args"`
}

func allowedChatTools() []chatToolSpec {
    return []chatToolSpec{
        {
            Name:        "learning_build",
            Description: "Start or rebuild a learning path from a material set.",
            Required:    []string{"material_set_id"},
            Optional:    []string{"path_id", "thread_id"},
        },
        {
            Name:        "learning_build_progressive",
            Description: "Start or rebuild a learning path progressively from a material set.",
            Required:    []string{"material_set_id"},
            Optional:    []string{"path_id", "thread_id"},
        },
        {
            Name:        "chat_rebuild",
            Description: "Rebuild chat projections for this thread.",
            Required:    []string{"thread_id"},
            Optional:    []string{},
        },
        {
            Name:        "chat_path_index",
            Description: "Reindex path artifacts for chat retrieval.",
            Required:    []string{"path_id"},
            Optional:    []string{},
        },
    }
}

func defaultString(v string, fallback string) string {
    if strings.TrimSpace(v) == "" {
        return fallback
    }
    return v
}
