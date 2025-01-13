package event

import (
	"fmt"

	"git.grassecon.net/grassrootseconomics/sarafu-api/remote"
)

type EventsHandler struct {
	api remote.AccountService
	formatFunc func(string, int, any) string
}

func NewEventsHandler(api remote.AccountService) *EventsHandler {
	return &EventsHandler{
		api: api,
		formatFunc: func(tag string, i int, o any) string {
			return fmt.Sprintf("%d %v", i, o)
		},
	}
}
