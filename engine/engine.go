package engine

import (
	"context"
	"fmt"

	"git.defalsify.org/vise.git/engine"
	"git.defalsify.org/vise.git/resource"
)

type SarafuEngine struct {
	engine.Engine
}

func NewSarafuEngine(cfg engine.Config, rs resource.Resource) *SarafuEngine {
	return &SarafuEngine{
		Engine: engine.NewEngine(cfg, rs),
	}
}

func (se *SarafuEngine) Exec(ctx context.Context, input []byte) (bool, error) {
	if len(input) == 0 {
		e, ok := se.Engine.(*engine.DefaultEngine)
		if !ok {
			return false, fmt.Errorf("")
		}

		e.Reset(ctx, true)
	}
	return se.Engine.Exec(ctx, input)
}
