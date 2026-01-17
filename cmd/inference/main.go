package main

import (
	"context"
	"fmt"
	"os"

	"github.com/yungbote/neurobridge-backend/internal/inference/app"
	"github.com/yungbote/neurobridge-backend/internal/inference/platform/shutdown"
)

func main() {
	a, err := app.New()
	if err != nil {
		fmt.Printf("failed to initialize app: %v\n", err)
		os.Exit(1)
	}

	ctx, stop := shutdown.NotifyContext(context.Background())
	defer stop()

	if err := a.Run(ctx); err != nil {
		fmt.Printf("server exited: %v\n", err)
		os.Exit(1)
	}
}
