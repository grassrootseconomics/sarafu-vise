package event

import (
	"fmt"

	"git.defalsify.org/vise.git/persist"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/store"
	"git.grassecon.net/grassrootseconomics/sarafu-api/remote"
	apievent "git.grassecon.net/grassrootseconomics/sarafu-api/event"
)

type EventsUpdater struct {
	api remote.AccountService
	formatFunc func(string, int, any) string
	store *store.UserDataStore
	pe *persist.Persister
}

func NewEventsUpdater(api remote.AccountService, store *store.UserDataStore, pe *persist.Persister) *EventsUpdater {
	return &EventsUpdater{
		api: api,
		formatFunc: func(tag string, i int, o any) string {
			return fmt.Sprintf("%d %v", i, o)
		},
		store: store,
		pe: pe,
	}
}

func (eu *EventsUpdater) ToEventsHandler() *apievent.EventsHandler {
	eh := apievent.NewEventsHandler()
	eh = eh.WithHandler(apievent.EventTokenTransferTag, eu.handleTokenTransfer)
	eh = eh.WithHandler(apievent.EventRegistrationTag, eu.handleCustodialRegistration)
	return eh
}
