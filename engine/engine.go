package engine

import (
	"context"

	"git.defalsify.org/vise.git/engine"
	"git.defalsify.org/vise.git/resource"
	"git.defalsify.org/vise.git/logging"
)

var (
	logg = logging.NewVanilla().WithDomain("sarafu-vise.engine")
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
	logg.TraceCtxf(ctx, "sarafu engine exec", "input", input)
//	if len(input) == 0 {
//		e := se.Engine.(*engine.DefaultEngine)
//		v, err := e.Reset(ctx, true)
//		if err != nil {
//			return v, err
//		}
//	}
	return se.Engine.Exec(ctx, input)
}
