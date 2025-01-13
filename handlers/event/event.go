package event

import (
	"git.grassecon.net/grassrootseconomics/sarafu-api/remote"
)

type EventsHandler struct {
	api remote.AccountService
	formatFunc func(string, int, any) string
}

func NewEventsHandler(api remote.AccountService) *EventsHandler {
	return &EventsHandler{
		api: api,
	}
}
