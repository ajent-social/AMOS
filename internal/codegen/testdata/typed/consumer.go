package apigen

import (
	"context"
	"errors"
)

type widgetCreator struct{}

var _ OpWidgetsCreateHandler = widgetCreator{}

func (widgetCreator) HandleWidgetsCreate(_ context.Context, req OpWidgetsCreateRequest) (OpWidgetsCreateResponse, error) {
	if req.Body.Priority < 0 {
		return OpWidgetsCreateResponse{}, errors.New("negative priority")
	}
	return OpWidgetsCreateResponse{Id: ModelWidgetId("created")}, nil
}
