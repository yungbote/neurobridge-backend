package router

import (
	"fmt"
	"strings"

	"github.com/yungbote/neurobridge-backend/internal/inference/config"
	"github.com/yungbote/neurobridge-backend/internal/inference/engine"
	"github.com/yungbote/neurobridge-backend/internal/inference/engine/mock"
	"github.com/yungbote/neurobridge-backend/internal/inference/engine/oaihttp"
)

type Route struct {
	PublicModel   string
	UpstreamModel string
	Engine        engine.Engine
}

type Router struct {
	routes map[string]Route
}

func New(cfg *config.Config) (*Router, error) {
	r := &Router{routes: map[string]Route{}}
	for _, m := range cfg.Models {
		id := strings.TrimSpace(m.ID)
		if id == "" {
			return nil, fmt.Errorf("model id required")
		}
		if _, exists := r.routes[id]; exists {
			return nil, fmt.Errorf("duplicate model id: %s", id)
		}

		var eng engine.Engine
		switch strings.ToLower(strings.TrimSpace(m.Engine.Type)) {
		case "mock":
			eng = mock.New()
		case "openai_http", "oai_http":
			e, err := oaihttp.New(m.Engine)
			if err != nil {
				return nil, err
			}
			eng = e
		default:
			return nil, fmt.Errorf("unsupported engine type %q for model %q", m.Engine.Type, id)
		}

		upstream := strings.TrimSpace(m.UpstreamModel)
		if upstream == "" {
			upstream = id
		}

		r.routes[id] = Route{
			PublicModel:   id,
			UpstreamModel: upstream,
			Engine:        eng,
		}
	}
	return r, nil
}

func (r *Router) ListModels() []string {
	out := make([]string, 0, len(r.routes))
	for id := range r.routes {
		out = append(out, id)
	}
	return out
}

func (r *Router) RouteForModel(model string) (Route, bool) {
	route, ok := r.routes[strings.TrimSpace(model)]
	return route, ok
}
