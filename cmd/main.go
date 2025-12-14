package main

import (
  "fmt"
  "os"
  "github.com/yungbote/neurobridge-backend/internal/app"
  "github.com/yungbote/neurobridge-backend/internal/utils"
)

func main() {
  a, err := app.New()
  if err != nil {
    fmt.Printf("Failed to initialize app: %v\n", err)
    os.Exit(1)
  }
  defer a.Close()
  a.Start()
  port := utils.GetEnv("PORT", "8080", a.Log)
  fmt.Printf("Server listening on :%s\n", port)
  if err := a.Run(":" + port); err != nil {
    a.Log.Warn("Server failed", "error", err)
  }
}










