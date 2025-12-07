package requestdata

import (
  "context"
  "github.com/google/uuid"
)

var requestDataKey = struct{}{}

func WithRequestData(ctx context.Context, rd *RequestData) context.Context {
  return context.WithValue(ctx, requestDataKey, rd)
}

func GetRequestData(ctx context.Context) *RequestData {
  val := ctx.Value(requestDataKey)
  if rd, ok := val.(*RequestData); ok {
    return rd
  }
  return nil
}

type RequestData struct {
  TokenString       string
  RefreshToken      string
  UserID            uuid.UUID
}

