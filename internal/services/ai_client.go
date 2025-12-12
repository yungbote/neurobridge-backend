package services

import (
  "bytes"
  "context"
  "encoding/json"
  "fmt"
  "io"
  "net/http"
  "time"
  "github.com/yungbote/neurobridge-backend/internal/logger"
  "github.com/yungbote/neurobridge-backend/internal/utils"
)

type AIClient interface {
  Chat(ctx context.Context, messages []AIMessage, opts *AIOptions) (*AICompletion, error)
  Embed(ctx context.Context, inputs []string) (*AIEmbeddingResult, error)
}

type AIMessage struct {
  Role	      string	  `json:"role"`
  Content     string	  `json:"content"`
}

type AIOptions struct {
  Temperature float32
  MaxTokens   int
}

type AICompletion struct {
  Content     string
  // add usage fields if want
}

type AIEmbeddingResult struct {
  Vectors     [][]float32
}

type aiClient struct {
  httpClient	  *http.Client
  log		  *logger.Logger
  apiKey	  string
  baseURL	  string
  chatModel	  string
  embeddingModel  string
}

func NewAIClient(log *logger.Logger) (AIClient, error) {
  serviceLog := log.With("service", "AIClient")
  apiKey := utils.GetEnv("OPENAI_API_KEY", "", log)
  if apiKey == "" {
    return nil, fmt.Errorf("OPENAI_API_KEY is not set")
  }
  baseURL := utils.GetEnv("OPENAI_BASE_URL", "https://api.openai.com/v1", log)
  chatModel := utils.GetEnv("OPENAI_CHAT_MODEL", "", log)
  embeddingModel := utils.GetEnv("OPENAI_EMBEDDING_MODEL", "", log)
  return &aiClient{
    httpClient: &httpClient{
      Timeout: 60 * time.Second,
    },
    log:	      serviceLog,
    apiKey:	      apiKey,
    baseURL:	      baseURL,
    chatModel:	      chatModel,
    embeddingModel:   embeddingModel,
  }, nil
}

func (c *aiClient) Chat(ctx context.Context, messages []AIMesa)










