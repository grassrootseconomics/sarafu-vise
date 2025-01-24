package event

import (
	"context"
	"fmt"

	"git.defalsify.org/vise.git/logging"
	"git.defalsify.org/vise.git/persist"
	apievent "git.grassecon.net/grassrootseconomics/sarafu-api/event"
	"git.grassecon.net/grassrootseconomics/sarafu-api/remote"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/store"
	"git.grassecon.net/grassrootseconomics/visedriver/storage"
)

var (
	logg = logging.NewVanilla().WithDomain("sarafu-vise.handlers.event")
)

type EventsUpdater struct {
	api        remote.AccountService
	formatFunc func(string, int, any) string
	store      storage.StorageService
}

func NewEventsUpdater(api remote.AccountService, store storage.StorageService) *EventsUpdater {
	return &EventsUpdater{
		api: api,
		formatFunc: func(tag string, i int, o any) string {
			return fmt.Sprintf("%d %v", i, o)
		},
		store: store,
	}
}

func (eu *EventsUpdater) ToEventsHandler() *apievent.EventsHandler {
	eh := apievent.NewEventsHandler()
	eh = eh.WithHandler(apievent.EventTokenMintTag, eu.handleTokenMint)
	eh = eh.WithHandler(apievent.EventTokenTransferTag, eu.handleTokenTransfer)
	eh = eh.WithHandler(apievent.EventRegistrationTag, eu.handleCustodialRegistration)
	return eh
}

func (eu *EventsUpdater) handleNoop(ctx context.Context, ev any) error {
	logg.WarnCtxf(ctx, "noop event handler")
	return nil
}

func (eu *EventsUpdater) getStore(ctx context.Context) (*persist.Persister, *store.UserDataStore, error) {
	userDb, err := eu.store.GetUserdataDb(ctx)
	if err != nil {
		return nil, nil, err
	}
	userStore := &store.UserDataStore{
		Db: userDb,
	}
	pr, err := eu.store.GetPersister(ctx)
	if err != nil {
		return nil, nil, err
	}
	return pr, userStore, nil
}
