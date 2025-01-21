// +build !online

package services

import (
	"fmt"
	"context"

	"git.grassecon.net/grassrootseconomics/visedriver/storage"
	devremote "git.grassecon.net/grassrootseconomics/sarafu-api/dev"
	"git.grassecon.net/grassrootseconomics/sarafu-api/remote"
	apievent "git.grassecon.net/grassrootseconomics/sarafu-api/event"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/handlers/event"
)

type localEmitter struct {
	h *apievent.EventsHandler
}

func (d *localEmitter) emit(ctx context.Context, msg apievent.Msg) error {
	var err error
	if msg.Typ == apievent.EventTokenTransferTag {
		tx, ok := msg.Item.(devremote.Tx)
		if !ok {
			return fmt.Errorf("not a valid tx")
		}
		ev := tx.ToTransferEvent()
		err = d.h.Handle(ctx, apievent.EventTokenTransferTag, &ev)
	} else if msg.Typ == apievent.EventRegistrationTag {
		acc, ok := msg.Item.(devremote.Account)
		if !ok {
			return fmt.Errorf("not a valid tx")
		}
		ev := acc.ToRegistrationEvent()
		err = d.h.Handle(ctx, apievent.EventRegistrationTag, &ev)
	}
	return err
}

func New(ctx context.Context, storageService storage.StorageService) remote.AccountService {
	svc := devremote.NewDevAccountService(ctx, storageService)
	svc = svc.WithAutoVoucher(ctx, "FOO", 42)
	eu := event.NewEventsUpdater(svc, storageService)
	emitter := &localEmitter{
		h: eu.ToEventsHandler(),
	}
	svc = svc.WithEmitter(emitter.emit)
	svc.AddVoucher(ctx, "BAR")
	return svc
}
