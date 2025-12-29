package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/yungbote/neurobridge-backend/internal/app"
	"github.com/yungbote/neurobridge-backend/internal/utils"
)

func envTrue(key string, def bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	return strings.EqualFold(v, "true") || v == "1" || strings.EqualFold(v, "yes")
}

func main() {
	a, err := app.New()
	if err != nil {
		fmt.Printf("Failed to initialize app: %v\n", err)
		os.Exit(1)
	}
	defer a.Close()

	runServer := envTrue("RUN_SERVER", true)
	runWorker := envTrue("RUN_WORKER", false)

	// Start background components (worker + redis forwarder)
	a.Start(runServer, runWorker)

	if runServer {
		port := utils.GetEnv("PORT", "8080", a.Log)
		fmt.Printf("Server listening on :%s\n", port)
		if err := a.Run(":" + port); err != nil {
			a.Log.Warn("Server failed", "error", err)
		}
		return
	}

	// Worker-only container: keep process alive.
	select {}
}




