package event

import (
	"context"
	"fmt"
	"strings"

	"git.defalsify.org/vise.git/db"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/store"
	storedb "git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"
	"git.grassecon.net/grassrootseconomics/common/identity"
	apievent "git.grassecon.net/grassrootseconomics/sarafu-api/event"
)

// execute all 
func (eu *EventsUpdater) updateToken(ctx context.Context, identity identity.Identity, userStore *store.UserDataStore, tokenAddress string) error {
	err := eu.updateTokenList(ctx, identity, userStore)
	if err != nil {
		return err
	}

	userStore.Db.SetSession(identity.SessionId)
	activeSym, err := userStore.ReadEntry(ctx, identity.SessionId, storedb.DATA_ACTIVE_SYM)
	if err == nil {
		return nil
	}
	if !db.IsNotFound(err) {
		return err
	}
	if activeSym == nil {
		activeSym, err = eu.toSym(ctx, tokenAddress)
		if err != nil {
			return err
		}
	}

	err = eu.updateDefaultToken(ctx, identity, userStore, string(activeSym))
	if err != nil {
		return err
	}

	err = eu.updateTokenTransferList(ctx, identity, userStore)
	if err != nil {
		return err
	}

	return nil
}


// set default token to given symbol.
func (eu *EventsUpdater) updateDefaultToken(ctx context.Context, identity identity.Identity, userStore *store.UserDataStore, activeSym string) error {
	pfxDb := toPrefixDb(userStore, identity.SessionId)
	// TODO: the activeSym input should instead be newline separated list?
	tokenData, err := store.GetVoucherData(ctx, pfxDb, activeSym)
	if err != nil {
		return err
	}
	return store.UpdateVoucherData(ctx, userStore, identity.SessionId, tokenData)
}


// handle token transfer.
//
// if from and to are NOT the same, handle code will be executed once for each side of the transfer.
func (eh *EventsUpdater) handleTokenTransfer(ctx context.Context, ev any) error {
	o, ok := ev.(*apievent.EventTokenTransfer)
	if !ok {
		fmt.Errorf("invalid event for custodial registration")
	}
	return eh.HandleTokenTransfer(ctx, o)
}
func (eu *EventsUpdater) HandleTokenTransfer(ctx context.Context, ev *apievent.EventTokenTransfer) error {
	_, userStore, err := eu.getStore(ctx)
	if err != nil {
		return err
	}
	identity, err := store.IdentityFromAddress(ctx, userStore, ev.From)
	if err != nil {
		if !db.IsNotFound(err) {
			return err
		}
	} else {
		err = eu.updateToken(ctx, identity, userStore, ev.VoucherAddress)
		if err != nil {
			return err
		}
	}

	if strings.Compare(ev.To, ev.From) != 0 {
		identity, err = store.IdentityFromAddress(ctx, userStore, ev.To)
		if err != nil {
			if !db.IsNotFound(err) {
				return err
			}
		} else {
			err = eu.updateToken(ctx, identity, userStore, ev.VoucherAddress)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// handle token mint.
func (eu *EventsUpdater) HandleTokenMint(ctx context.Context, ev *apievent.EventTokenMint) error {
	_, userStore, err := eu.getStore(ctx)
	if err != nil {
		return err
	}
	identity, err := store.IdentityFromAddress(ctx, userStore, ev.To)
	if err != nil {
		if !db.IsNotFound(err) {
			return err
		}
	} else {
		err = eu.updateToken(ctx, identity, userStore, ev.VoucherAddress)
		if err != nil {
			return err
		}
	}
	return nil
}

// use api to resolve address to token symbol.
func (eu *EventsUpdater) toSym(ctx context.Context, address string) ([]byte, error) {
	voucherData, err := eu.api.VoucherData(ctx, address)
	if err != nil {
		return nil, err
	}
	return []byte(voucherData.TokenSymbol), nil
}

// refresh and store token list.
func (eu *EventsUpdater) updateTokenList(ctx context.Context, identity identity.Identity, userStore *store.UserDataStore) error {
	holdings, err := eu.api.FetchVouchers(ctx, identity.ChecksumAddress)
	if err != nil {
		return err
	}
	metadata := store.ProcessVouchers(holdings)
	_ = metadata

	// TODO: make sure subprefixdb is thread safe when using gdbm
	// TODO: why is address session here unless explicitly set
	pfxDb := toPrefixDb(userStore, identity.SessionId)

	typ := storedb.ToBytes(storedb.DATA_VOUCHER_SYMBOLS)
	err = pfxDb.Put(ctx, typ, []byte(metadata.Symbols))
	if err != nil {
		return err
	}

	typ = storedb.ToBytes(storedb.DATA_VOUCHER_BALANCES)
	err = pfxDb.Put(ctx, typ, []byte(metadata.Balances))
	if err != nil {
		return err
	}

	typ = storedb.ToBytes(storedb.DATA_VOUCHER_DECIMALS)
	err = pfxDb.Put(ctx, typ, []byte(metadata.Decimals))
	if err != nil {
		return err
	}

	typ = storedb.ToBytes(storedb.DATA_VOUCHER_ADDRESSES)
	err = pfxDb.Put(ctx, typ, []byte(metadata.Addresses))
	if err != nil {
		return err
	}

	return nil
}

// refresh and store transaction history.
func (eu *EventsUpdater) updateTokenTransferList(ctx context.Context, identity identity.Identity, userStore *store.UserDataStore) error {
	var r []string

	txs, err := eu.api.FetchTransactions(ctx, identity.ChecksumAddress)
	if err != nil {
		return err
	}

	for i, tx := range(txs) {
		r = append(r, eu.formatFunc(apievent.EventTokenTransferTag, i, tx))
	}

	s := strings.Join(r, "\n")

	return userStore.WriteEntry(ctx, identity.SessionId, storedb.DATA_TRANSACTIONS, []byte(s))
}

func toPrefixDb(userStore *store.UserDataStore, sessionId string) storedb.PrefixDb {
	userStore.Db.SetSession(sessionId)
	prefix := storedb.ToBytes(db.DATATYPE_USERDATA)
	return store.StoreToPrefixDb(userStore, prefix)
}
